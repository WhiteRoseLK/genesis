// SPDX-License-Identifier: Apache-2.0

// Package engine runs the plan: Check before any action, state persisted
// after each successful step, resumption (docs/02-architecture.md: Engine).
//
// Accepted simplification for milestone M4: a single Check per module
// decides whether to replay its whole group of steps (seed.up, provision,
// configure, verify, [handover]) rather than a Check before each individual
// RPC — since each step is itself idempotent (docs/03-module-contract.md §4,
// rule 3), replaying the group after a resumption is safe even if some
// steps of the group had already succeeded.
//
// Multi-module handover (docs/05-bootstrap-lifecycle.md): a target module
// one of whose provided functions has an active seed provider
// (resolver.Resolved.ProviderFor(fn, "seed")) runs Handover, then becomes
// the function's active provider for every module built afterwards
// (registry key without the @phase suffix, see repoint). The seed itself
// stays active: its own services keep using it. It is only stopped at the
// end of Run (retireSeed, ADR-020), once every seed function has been taken
// over by the target and every target Verify is green. The proto's
// explicit Repoint(RepointRequest) is not yet triggered by the core towards
// consumer modules: in this MVP, no module consumes dns.zone/v1,
// dns.resolver/v1 or time.ntp/v1 without the phase suffix after its own
// Check (the only consumer sensitive to the handover, powerdns, explicitly
// reads dns.zone/v1@seed) — Repoint will have to be wired as soon as such a
// consumer exists (debt, docs/PROGRESS.md).
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/WhiteRoseLK/genesis/internal/broker"
	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/planner"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/secrets"
	"github.com/WhiteRoseLK/genesis/internal/state"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	secretskvv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/secrets/kv/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// providerConn is the connection dispensed by a module for a function it
// provides, with its forwarder — cached for repoint (no re-dispensing, see
// Run).
type providerConn struct {
	conn     *grpc.ClientConn
	register func(*grpc.Server, *grpc.ClientConn)
}

// Engine runs a resolved plan.
type Engine struct {
	Registry *broker.Registry
	StateDir string
	Logger   *slog.Logger
	// Secrets: direct access (not only through core.secrets/v1) for the
	// file->vault migration (docs/06-secrets-state.md) — only the core can
	// list ALL entries, a module only accesses its own.
	Secrets secrets.Store
	Router  *secrets.Router
}

// New builds an engine: a broker registry with core.secrets/v1 already
// registered (a native function, never a module). If secretsStore is a FileStore,
// it is wrapped in a Router so core.secrets/v1 can dynamically route between
// the file backend and secrets.kv/v1 (docs/05-bootstrap-lifecycle.md, ADR-020).
func New(stateDir string, secretsStore secrets.Store) *Engine {
	registry := broker.NewRegistry()
	var router *secrets.Router
	if r, ok := secretsStore.(*secrets.Router); ok {
		router = r
	} else if fs, ok := secretsStore.(*secrets.FileStore); ok {
		router = secrets.NewRouter(fs)
		secretsStore = router
	}
	registry.SetNative("core.secrets/v1", broker.NativeSecrets(secretsStore))
	return &Engine{Registry: registry, StateDir: stateDir, Logger: slog.Default(), Secrets: secretsStore, Router: router}
}

func (e *Engine) fileStore() (*secrets.FileStore, bool) {
	if e.Router != nil {
		return e.Router.FileStore(), true
	}
	fs, ok := e.Secrets.(*secrets.FileStore)
	return fs, ok
}

// Run executes plan in order, holding the state lock (two concurrent
// applies → the second one refuses, docs/06-secrets-state.md).
func (e *Engine) Run(ctx context.Context, resolved *resolver.Resolved, plan *planner.Plan) error {
	release, err := state.Lock(e.StateDir)
	if err != nil {
		return err
	}
	defer func() { _ = release() }()

	st, err := state.Load(e.StateDir)
	if errors.Is(err, state.ErrNotInitialized) {
		st = state.New()
	} else if err != nil {
		return err
	}
	if st.Modules == nil {
		st.Modules = map[string]state.ModuleState{}
	}
	if st.SecretsBackend == "vault" || st.SeedRetired {
		if fs, ok := e.fileStore(); ok {
			fs.SetReadOnly(true)
		}
	}

	clients := map[string]*modulehost.Client{}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	// providers[function][module]: dispensed connection, cached for the
	// post-handover repoint (no re-dispensing, see repoint below).
	providers := map[string]map[string]providerConn{}

	for _, name := range plan.Order {
		m := resolved.Modules[name]
		client, err := modulehost.Launch(m.Installed.BinaryPath, m.Manifest)
		if err != nil {
			return fmt.Errorf("launching module %q: %w", name, err)
		}
		clients[name] = client

		for _, p := range m.Manifest.Provides {
			conn, err := client.DispenseFunction(p.Function)
			if err != nil {
				return fmt.Errorf("connecting to function %q of module %q: %w", p.Function, name, err)
			}
			if p.Function == "secrets.kv/v1" && st.SecretsBackend == "vault" && e.Router != nil {
				e.Router.SwitchToVault(secretskvv1.NewSecretsKVClient(conn))
			}
			if p.Fleet {
				// "Fleet" function (ADR-017): every accumulated connection
				// is called (fan-out), not a single chosen/repointable
				// active provider — no @phase key and no bare "active"
				// key, that concept does not exist here.
				e.Registry.AddFleetProvider(p.Function, conn)
				continue
			}
			register, ok := broker.ForwarderFor(p.Function)
			if !ok {
				return fmt.Errorf("function %q (module %q): function type unknown to the core", p.Function, name)
			}
			// Phase-qualified key: lets a target module explicitly
			// consume ANOTHER module's @seed function during its handover
			// (e.g. powerdns reads dns.zone/v1@seed from coredns), even
			// when a third module provides the same function in the target
			// phase (docs/05-bootstrap-lifecycle.md).
			for _, phase := range p.Phases {
				e.Registry.SetModuleProvider(p.Function+"@"+phase, conn, register)
			}
			if providers[p.Function] == nil {
				providers[p.Function] = map[string]providerConn{}
			}
			providers[p.Function][name] = providerConn{conn: conn, register: register}
		}
	}

	// "Active" key (no @phase suffix): the seed while it exists (repointed
	// to the target after the handover, see repoint), otherwise directly the
	// target for functions without a seed phase (e.g. compute.vm/v1) or once
	// the seed has been retired.
	for function, byModule := range providers {
		phase := "seed"
		if _, ok := resolved.ProviderFor(function, "seed"); !ok || st.SeedRetired {
			phase = "target"
		}
		providerName, ok := resolved.ProviderFor(function, phase)
		if !ok {
			continue
		}
		if pc, ok := byModule[providerName]; ok {
			e.Registry.SetModuleProvider(function, pc.conn, pc.register)
		}
	}

	runID, err := newRunID()
	if err != nil {
		return err
	}

	for _, name := range plan.Order {
		m := resolved.Modules[name]
		if err := e.runModule(ctx, runID, resolved, m, clients, providers, st); err != nil {
			return err
		}
	}

	return e.retireSeed(ctx, runID, resolved, plan, clients, st)
}

// Destroy deletes the target resources created by the modules in reverse plan
// order (docs/02-architecture.md: genesis destroy).
func (e *Engine) Destroy(ctx context.Context, resolved *resolver.Resolved, plan *planner.Plan) error {
	st, err := state.Load(e.StateDir)
	if err != nil {
		return err
	}
	if st.Modules == nil {
		st.Modules = map[string]state.ModuleState{}
	}
	if st.SecretsBackend == "vault" || st.SeedRetired {
		if fs, ok := e.fileStore(); ok {
			fs.SetReadOnly(true)
		}
	}

	clients := map[string]*modulehost.Client{}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	providers := map[string]map[string]providerConn{}

	for _, name := range plan.Order {
		m := resolved.Modules[name]
		client, err := modulehost.Launch(m.Installed.BinaryPath, m.Manifest)
		if err != nil {
			return fmt.Errorf("launching module %q: %w", name, err)
		}
		clients[name] = client

		for _, p := range m.Manifest.Provides {
			conn, err := client.DispenseFunction(p.Function)
			if err != nil {
				return fmt.Errorf("connecting to function %q of module %q: %w", p.Function, name, err)
			}
			if p.Fleet {
				e.Registry.AddFleetProvider(p.Function, conn)
				continue
			}
			register, ok := broker.ForwarderFor(p.Function)
			if !ok {
				return fmt.Errorf("function %q (module %q): function type unknown to the core", p.Function, name)
			}
			for _, phase := range p.Phases {
				e.Registry.SetModuleProvider(p.Function+"@"+phase, conn, register)
			}
			if providers[p.Function] == nil {
				providers[p.Function] = map[string]providerConn{}
			}
			providers[p.Function][name] = providerConn{conn: conn, register: register}
		}
	}

	for function, byModule := range providers {
		phase := "target"
		if _, ok := resolved.ProviderFor(function, "target"); !ok {
			phase = "seed"
		}
		providerName, ok := resolved.ProviderFor(function, phase)
		if !ok {
			continue
		}
		if pc, ok := byModule[providerName]; ok {
			e.Registry.SetModuleProvider(function, pc.conn, pc.register)
		}
	}

	runID, err := newRunID()
	if err != nil {
		return err
	}

	for i := len(plan.Order) - 1; i >= 0; i-- {
		name := plan.Order[i]
		m := resolved.Modules[name]
		client := clients[m.Name]

		req, err := e.buildStepRequest(runID, m, client, st)
		if err != nil {
			return err
		}

		result, err := client.Module().Destroy(ctx, req)
		if err != nil {
			return modulehost.WrapModuleError("destroy", err)
		}
		if result.GetStatus() != modulev1.StepResult_STATUS_OK {
			return fmt.Errorf("module %q, step destroy: failed (%s)", m.Name, diagnosticsString(result.GetDiagnostics()))
		}
		delete(st.Modules, m.Name)
		if err := state.Save(e.StateDir, st); err != nil {
			return err
		}
		e.audit(m.Name, "destroy", "ok")
	}

	return nil
}

func (e *Engine) runModule(
	ctx context.Context,
	runID string,
	resolved *resolver.Resolved,
	m *resolver.Module,
	clients map[string]*modulehost.Client,
	providers map[string]map[string]providerConn,
	st *state.State,
) error {
	client := clients[m.Name]
	if st.SeedRetired && !providesInPhase(m, "target") {
		// Pure seed module already retired: neither Check nor SeedUp, it
		// must never restart.
		e.audit(m.Name, "check", "seed retired, skipped")
		return nil
	}
	req, err := e.buildStepRequest(runID, m, client, st)
	if err != nil {
		return err
	}

	checkResult, err := client.Module().Check(ctx, req)
	if err != nil {
		return modulehost.WrapModuleError("check", err)
	}
	if checkResult.GetStatus() == modulev1.CheckResult_STATUS_COMPLIANT {
		e.audit(m.Name, "check", "compliant, no action")
		// A fresh registry on every Run: without this repoint, a module
		// built later in this Run would still consume the seed.
		e.repoint(resolved, m, providers)
		return nil
	}

	if providesInPhase(m, "seed") && !st.SeedRetired {
		if err := e.action(ctx, runID, m, client, st, "seed.up", client.Module().SeedUp); err != nil {
			return err
		}
	}

	if !providesInPhase(m, "target") {
		// Pure seed module (e.g. coredns): stops at seed_ready
		// (docs/03-module-contract.md §5) — SeedDown only happens at the end
		// of Run (retireSeed).
		return e.action(ctx, runID, m, client, st, "verify", client.Module().Verify)
	}

	if err := e.action(ctx, runID, m, client, st, "provision", client.Module().Provision); err != nil {
		return err
	}
	if err := e.action(ctx, runID, m, client, st, "configure", client.Module().Configure); err != nil {
		return err
	}
	if err := e.action(ctx, runID, m, client, st, "verify", client.Module().Verify); err != nil {
		return err
	}

	if err := e.migrateSecretsIfNeeded(ctx, resolved, m, client, st); err != nil {
		return err
	}

	if len(handoverSeedModules(resolved, m)) == 0 {
		return nil // target_ready -> done: no handover (docs/03 §5).
	}
	if !st.SeedRetired {
		// After retirement, the seed no longer exists: Handover (which may
		// read it, e.g. powerdns through dns.zone/v1@seed) is pointless.
		if err := e.action(ctx, runID, m, client, st, "handover", client.Module().Handover); err != nil {
			return err
		}
	}
	e.repoint(resolved, m, providers)
	return nil
}

// retireSeed stops the seed at the end of Run (docs/05-bootstrap-lifecycle.md,
// phase 4; ADR-020), without operator intervention, but only if:
//   - every function provided in the seed phase has a target provider
//     (otherwise the seed remains the only provider and is kept);
//   - the "secrets" capability, if present, has migrated to vault;
//   - a final Verify of each target module, in the final configuration
//     (all repoints done), is green.
//
// Seed modules are stopped in reverse plan order (a seed service may depend
// on another, e.g. step-ca on coredns's DNS).
func (e *Engine) retireSeed(ctx context.Context, runID string, resolved *resolver.Resolved, plan *planner.Plan, clients map[string]*modulehost.Client, st *state.State) error {
	if st.SeedRetired {
		return nil
	}
	var seedModules, targetModules []string
	for _, name := range plan.Order {
		m := resolved.Modules[name]
		if providesInPhase(m, "seed") {
			seedModules = append(seedModules, name)
		}
		if providesInPhase(m, "target") {
			targetModules = append(targetModules, name)
		}
	}
	if len(seedModules) == 0 {
		return nil
	}

	if missing := functionsWithoutTarget(resolved, seedModules); len(missing) > 0 {
		e.audit("seed", "seed.retire", "kept: no target successor for "+strings.Join(missing, ", "))
		return nil
	}
	if resolved.CapabilityModule["secrets"] != "" && st.SecretsBackend != "vault" {
		e.audit("seed", "seed.retire", "kept: secrets not yet migrated to vault")
		return nil
	}

	for _, name := range targetModules {
		if err := e.action(ctx, runID, resolved.Modules[name], clients[name], st, "verify.final", clients[name].Module().Verify); err != nil {
			return fmt.Errorf("seed kept, final verification failed: %w", err)
		}
	}

	for i := len(seedModules) - 1; i >= 0; i-- {
		name := seedModules[i]
		if err := e.action(ctx, runID, resolved.Modules[name], clients[name], st, "seed.retire", clients[name].Module().SeedDown); err != nil {
			return err
		}
	}

	st.SeedRetired = true
	if err := state.Save(e.StateDir, st); err != nil {
		return fmt.Errorf("persisting the seed retirement: %w", err)
	}
	if fs, ok := e.fileStore(); ok {
		fs.SetReadOnly(true)
	}
	e.audit("seed", "seed.retire", "seed retired")
	return nil
}

// functionsWithoutTarget lists, sorted, the functions provided in the seed
// phase by seedModules that have no provider in the target phase.
func functionsWithoutTarget(resolved *resolver.Resolved, seedModules []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range seedModules {
		for _, p := range resolved.Modules[name].Manifest.Provides {
			if !slices.Contains(p.Phases, "seed") || seen[p.Function] {
				continue
			}
			seen[p.Function] = true
			if _, ok := resolved.ProviderFor(p.Function, "target"); !ok {
				out = append(out, p.Function)
			}
		}
	}
	sort.Strings(out)
	return out
}

// handoverSeedModules returns, deduplicated and sorted, the distinct seed
// modules for which AT LEAST one function provided by m in the target phase
// had an active provider in the seed phase — that is the very definition of
// a handover (docs/05-bootstrap-lifecycle.md). A module that provides the
// same function in both phases (self-handover, e.g. test/modules/test-a)
// appears in the list itself: Handover and SeedDown then both run on it.
func handoverSeedModules(resolved *resolver.Resolved, m *resolver.Module) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range m.Manifest.Provides {
		if !slices.Contains(p.Phases, "target") {
			continue
		}
		seedName, ok := resolved.ProviderFor(p.Function, "seed")
		if !ok || seen[seedName] {
			continue
		}
		seen[seedName] = true
		out = append(out, seedName)
	}
	sort.Strings(out)
	return out
}

// repoint makes m the new active provider (key without the @phase suffix)
// of each of its target functions that has just been taken over from a seed
// provider — it reuses the connection already dispensed at launch, with no
// re-dispensing.
func (e *Engine) repoint(resolved *resolver.Resolved, m *resolver.Module, providers map[string]map[string]providerConn) {
	for _, p := range m.Manifest.Provides {
		if !slices.Contains(p.Phases, "target") {
			continue
		}
		if _, ok := resolved.ProviderFor(p.Function, "seed"); !ok {
			continue
		}
		if pc, ok := providers[p.Function][m.Name]; ok {
			e.Registry.SetModuleProvider(p.Function, pc.conn, pc.register)
		}
	}
}

// migrateSecretsIfNeeded implements docs/06-secrets-state.md: "file → vault
// migration, triggered by the handover of the secrets capability" — this is
// NOT a classic function handover (secrets.kv/v1 has no seed provider to
// take over from; it is core.secrets/v1, native, that changes backend), so
// handoverSeedModules/repoint do not cover it: as soon as the module chosen
// for the "secrets" capability provides secrets.kv/v1 as a target and has
// just passed Provision/Configure/Verify, each non-`recovery` secret of the
// file backend is copied, verified by reading it back, then the active
// backend switches in the state.
func (e *Engine) migrateSecretsIfNeeded(ctx context.Context, resolved *resolver.Resolved, m *resolver.Module, client *modulehost.Client, st *state.State) error {
	if resolved.CapabilityModule["secrets"] != m.Name {
		return nil
	}
	if !providesFunction(m, "secrets.kv/v1") {
		return nil
	}
	if st.SecretsBackend == "vault" {
		return nil // already migrated (idempotent).
	}

	conn, err := client.DispenseFunction("secrets.kv/v1")
	if err != nil {
		return fmt.Errorf("migrating secrets to %q: %w", m.Name, err)
	}
	kv := secretskvv1.NewSecretsKVClient(conn)

	entries, err := e.Secrets.List(ctx, "")
	if err != nil {
		return fmt.Errorf("migrating secrets: listing the file backend: %w", err)
	}

	migrated := 0
	for _, entry := range entries {
		if entry.Meta.Recovery {
			continue // doc 06: "Recovery: true entries stay in file".
		}
		value, err := e.Secrets.Get(ctx, entry.Ref)
		if err != nil {
			return fmt.Errorf("migrating %q: reading from file: %w", entry.Ref, err)
		}
		if _, err := kv.Write(ctx, &secretskvv1.WriteRequest{Ref: string(entry.Ref), Value: value.ExposeSecret()}); err != nil {
			return fmt.Errorf("migrating %q: writing to vault: %w", entry.Ref, err)
		}
		readBack, err := kv.Read(ctx, &secretskvv1.ReadRequest{Ref: string(entry.Ref)})
		if err != nil {
			return fmt.Errorf("migrating %q: reading back from vault: %w", entry.Ref, err)
		}
		if !readBack.GetFound() || readBack.GetValue() != value.ExposeSecret() {
			return fmt.Errorf("migrating %q: the value read back from vault does not match the value written", entry.Ref)
		}
		migrated++
	}

	st.SecretsBackend = "vault"
	if err := state.Save(e.StateDir, st); err != nil {
		return fmt.Errorf("persisting the secret backend after migration: %w", err)
	}
	if e.Router != nil {
		e.Router.SwitchToVault(kv)
	} else if fs, ok := e.fileStore(); ok {
		fs.SetReadOnly(true)
	}
	e.audit(m.Name, "secrets.migrate", fmt.Sprintf("%d entry(ies) migrated to vault", migrated))
	return nil
}

func providesFunction(m *resolver.Module, function string) bool {
	for _, p := range m.Manifest.Provides {
		if p.Function == function {
			return true
		}
	}
	return false
}

func (e *Engine) action(
	ctx context.Context,
	runID string,
	m *resolver.Module,
	client *modulehost.Client,
	st *state.State,
	stepName string,
	rpc func(context.Context, *modulev1.StepRequest, ...grpc.CallOption) (*modulev1.StepResult, error),
) error {
	req, err := e.buildStepRequest(runID, m, client, st)
	if err != nil {
		return err
	}
	result, err := rpc(ctx, req)
	if err != nil {
		return modulehost.WrapModuleError(stepName, err)
	}
	if result.GetStatus() != modulev1.StepResult_STATUS_OK {
		return fmt.Errorf("module %q, step %s: failed (%s)", m.Name, stepName, diagnosticsString(result.GetDiagnostics()))
	}
	if err := e.saveModuleState(m.Name, result.GetState(), st); err != nil {
		return err
	}
	e.audit(m.Name, stepName, "ok")
	return nil
}

func (e *Engine) buildStepRequest(runID string, m *resolver.Module, client *modulehost.Client, st *state.State) (*modulev1.StepRequest, error) {
	token := e.Registry.OpenSession(client.Broker(), m.Name, requiredFunctions(m.Manifest))

	var stateStruct *structpb.Struct
	if ms, ok := st.Modules[m.Name]; ok && len(ms.StateJSON) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(ms.StateJSON, &raw); err != nil {
			return nil, fmt.Errorf("invalid persisted state for %q: %w", m.Name, err)
		}
		s, err := structpb.NewStruct(raw)
		if err != nil {
			return nil, fmt.Errorf("encoding the state of %q: %w", m.Name, err)
		}
		stateStruct = s
	}

	moduleConfig := m.Config
	if moduleConfig == nil {
		moduleConfig = map[string]any{}
	}
	resolvedConfig, err := resolveRefs(moduleConfig)
	if err != nil {
		return nil, fmt.Errorf("resolving the references of %q: %w", m.Name, err)
	}
	config, err := structpb.NewStruct(resolvedConfig)
	if err != nil {
		return nil, fmt.Errorf("encoding the config of %q: %w", m.Name, err)
	}

	return &modulev1.StepRequest{
		RunId:       runID,
		Config:      config,
		State:       stateStruct,
		BrokerToken: token,
	}, nil
}

func (e *Engine) saveModuleState(moduleName string, s *structpb.Struct, st *state.State) error {
	raw, err := json.Marshal(s.AsMap())
	if err != nil {
		return fmt.Errorf("encoding the state of %q: %w", moduleName, err)
	}
	st.Modules[moduleName] = state.ModuleState{StateJSON: raw}
	if err := state.Save(e.StateDir, st); err != nil {
		return fmt.Errorf("persisting the state after the step of %q: %w", moduleName, err)
	}
	return nil
}

func (e *Engine) audit(module, step, outcome string) {
	e.Logger.Info("step", "module", module, "step", step, "outcome", outcome)
}

func providesInPhase(m *resolver.Module, phase string) bool {
	for _, p := range m.Manifest.Provides {
		for _, ph := range p.Phases {
			if ph == phase {
				return true
			}
		}
	}
	return false
}

// requiredFunctions returns the functions declared in requires, as is —
// including the @seed/@target suffix when present (docs/03 §1: forces the
// provider): the matching registry key only exists in that qualified form
// (see Run, per-phase registration).
func requiredFunctions(m *sdk.ManifestFile) []string {
	seen := map[string]bool{}
	var out []string
	for _, entries := range m.Requires {
		for _, entry := range entries {
			if !seen[entry.Function] {
				seen[entry.Function] = true
				out = append(out, entry.Function)
			}
		}
	}
	sort.Strings(out)
	return out
}

func diagnosticsString(diags []*modulev1.Diagnostic) string {
	if len(diags) == 0 {
		return "no diagnostics"
	}
	parts := make([]string, len(diags))
	for i, d := range diags {
		parts[i] = fmt.Sprintf("%s: %s", d.GetPath(), d.GetMessage())
	}
	return strings.Join(parts, "; ")
}

func newRunID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating the run ID: %w", err)
	}
	return "run-" + hex.EncodeToString(buf), nil
}
