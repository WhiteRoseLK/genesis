// SPDX-License-Identifier: Apache-2.0

// Package engine exécute le plan : Check avant toute action, état persisté
// après chaque étape réussie, reprise (docs/02-architecture.md : Moteur).
//
// Simplification assumée pour le jalon J4 : un seul Check par module décide
// s'il faut rejouer son groupe d'étapes en entier (seed.up, provision,
// configure, verify, [handover, seed.retire]) plutôt qu'un Check devant
// chaque RPC individuellement — chaque étape étant elle-même idempotente
// (docs/03-contrat-module.md §4, règle 3), rejouer le groupe après une
// reprise est sûr même si certaines étapes du groupe avaient déjà réussi.
//
// Passation multi-module (docs/05-cycle-bootstrap.md), depuis le jalon J6 :
// un module cible dont une fonction fournie a un fournisseur graine actif
// (resolver.Resolved.ProviderFor(fn, "seed")) exécute Handover puis
// déclenche SeedDown sur CE fournisseur graine (potentiellement un module
// différent, ex. coredns → powerdns) avant de devenir le nouveau
// fournisseur actif de la fonction (clé de registre sans suffixe @phase,
// voir repoint). Le Repoint(RepointRequest) explicite du proto n'est pas
// encore déclenché par le cœur vers les modules consommateurs : dans ce
// MVP, aucun module ne consomme encore dns.zone/v1, dns.resolver/v1 ou
// time.ntp/v1 sans le suffixe de phase après son propre Check (le seul
// consommateur sensible à la passation, powerdns, lit explicitement
// dns.zone/v1@seed) — Repoint devra être câblé dès qu'un tel consommateur
// existera (dette, docs/PROGRESS.md).
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

	"genesis/internal/broker"
	"genesis/internal/modulehost"
	"genesis/internal/planner"
	"genesis/internal/resolver"
	"genesis/internal/secrets"
	"genesis/internal/state"
	sdk "genesis/sdk/go"
	secretskvv1 "genesis/sdk/go/gen/functions/secrets/kv/v1"
	modulev1 "genesis/sdk/go/gen/module/v1"
)

// providerConn est la connexion dispensée d'un module pour une fonction
// qu'il fournit, avec son forwarder — mise en cache pour repoint (pas de
// redispense, voir Run).
type providerConn struct {
	conn     *grpc.ClientConn
	register func(*grpc.Server, *grpc.ClientConn)
}

// Engine exécute un plan résolu.
type Engine struct {
	Registry *broker.Registry
	StateDir string
	Logger   *slog.Logger
	// Secrets : accès direct (pas seulement via core.secrets/v1) pour la
	// migration file->vault (docs/06-secrets-etat.md) — seul le cœur peut
	// lister TOUTES les entrées, un module n'accède qu'aux siennes.
	Secrets secrets.Store
}

// New construit un moteur : registre du broker avec core.secrets/v1 déjà
// enregistrée (fonction native, jamais un module).
func New(stateDir string, secretsStore secrets.Store) *Engine {
	registry := broker.NewRegistry()
	registry.SetNative("core.secrets/v1", broker.NativeSecrets(secretsStore))
	return &Engine{Registry: registry, StateDir: stateDir, Logger: slog.Default(), Secrets: secretsStore}
}

// Run exécute plan dans l'ordre, avec le verrou d'état
// (deux apply concurrents → le second refuse, docs/06-secrets-etat.md).
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

	clients := map[string]*modulehost.Client{}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	// providers[fonction][module] : connexion dispensée, mise en cache pour
	// le repoint post-passation (pas de redispense, voir repoint plus bas).
	providers := map[string]map[string]providerConn{}

	for _, name := range plan.Order {
		m := resolved.Modules[name]
		client, err := modulehost.Launch(m.Installed.BinaryPath, m.Manifest)
		if err != nil {
			return fmt.Errorf("lancement du module %q : %w", name, err)
		}
		clients[name] = client

		for _, p := range m.Manifest.Provides {
			conn, err := client.DispenseFunction(p.Function)
			if err != nil {
				return fmt.Errorf("connexion à la fonction %q du module %q : %w", p.Function, name, err)
			}
			register, ok := broker.ForwarderFor(p.Function)
			if !ok {
				return fmt.Errorf("fonction %q (module %q) : type de fonction inconnu du cœur", p.Function, name)
			}
			// Clé qualifiée par phase : permet à un module cible de
			// consommer explicitement la fonction @seed d'un AUTRE module
			// pendant sa passation (ex. powerdns lit dns.zone/v1@seed chez
			// coredns), même quand un troisième module fournit la même
			// fonction en phase cible (docs/05-cycle-bootstrap.md).
			for _, phase := range p.Phases {
				e.Registry.SetModuleProvider(p.Function+"@"+phase, conn, register)
			}
			if providers[p.Function] == nil {
				providers[p.Function] = map[string]providerConn{}
			}
			providers[p.Function][name] = providerConn{conn: conn, register: register}
		}
	}

	// Clé "active" (sans suffixe @phase) : la graine tant qu'elle existe
	// (repointée vers la cible après passation, voir repoint), sinon
	// directement la cible pour les fonctions sans phase graine (ex.
	// compute.vm/v1).
	for function, byModule := range providers {
		phase := "seed"
		if _, ok := resolved.ProviderFor(function, "seed"); !ok {
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
	req, err := e.buildStepRequest(runID, m, client, st)
	if err != nil {
		return err
	}

	checkResult, err := client.Module().Check(ctx, req)
	if err != nil {
		return modulehost.WrapModuleError("check", err)
	}
	if checkResult.GetStatus() == modulev1.CheckResult_STATUS_CONFORME {
		e.audit(m.Name, "check", "conforme, aucune action")
		return nil
	}

	if providesInPhase(m, "seed") {
		if err := e.action(ctx, runID, m, client, st, "seed.up", client.Module().SeedUp); err != nil {
			return err
		}
	}

	if !providesInPhase(m, "target") {
		// Module graine pur (ex. coredns) : s'arrête à seed_ready
		// (docs/03-contrat-module.md §5) — une passation ultérieure d'un
		// module cible appellera SeedDown en temps voulu (voir plus bas).
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

	seedModules := handoverSeedModules(resolved, m)
	if len(seedModules) == 0 {
		return nil // target_ready -> done : pas de passation (docs/03 §5).
	}
	if err := e.action(ctx, runID, m, client, st, "handover", client.Module().Handover); err != nil {
		return err
	}
	for _, seedName := range seedModules {
		seedModule := resolved.Modules[seedName]
		seedClient := clients[seedName]
		if seedClient == nil {
			return fmt.Errorf("passation de %q : module graine %q introuvable", m.Name, seedName)
		}
		if err := e.action(ctx, runID, seedModule, seedClient, st, "seed.retire", seedClient.Module().SeedDown); err != nil {
			return err
		}
	}
	e.repoint(resolved, m, providers)
	return nil
}

// handoverSeedModules retourne, dédupliqué et trié, les modules graine
// distincts dont AU MOINS une fonction fournie par m en phase cible avait
// un fournisseur actif en phase graine — c'est la définition même d'une
// passation (docs/05-cycle-bootstrap.md). Un module qui fournit une même
// fonction dans les deux phases (auto-passation, ex. test/modules/test-a)
// s'y retrouve lui-même : Handover puis SeedDown s'exécutent alors tous
// deux sur lui.
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

// repoint fait de m le nouveau fournisseur actif (clé sans suffixe @phase)
// de chacune de ses fonctions cible qui vient d'être reprise d'un
// fournisseur graine — réutilise la connexion déjà dispensée au lancement,
// aucune redispense.
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

// migrateSecretsIfNeeded implémente docs/06-secrets-etat.md : "Migration
// file -> vault, déclenchée par la passation de la capacité secrets" — ce
// n'est PAS un handover de fonction classique (secrets.kv/v1 n'a pas de
// fournisseur graine à reprendre, c'est core.secrets/v1, natif, qui change
// de backend), donc pas couvert par handoverSeedModules/repoint : dès que
// le module choisi pour la capacité "secrets" fournit secrets.kv/v1 en
// cible et vient de passer Provision/Configure/Verify, chaque secret non
// `recovery` du backend file est copié, vérifié par relecture, puis le
// backend actif bascule dans l'état.
func (e *Engine) migrateSecretsIfNeeded(ctx context.Context, resolved *resolver.Resolved, m *resolver.Module, client *modulehost.Client, st *state.State) error {
	if resolved.CapabilityModule["secrets"] != m.Name {
		return nil
	}
	if !providesFunction(m, "secrets.kv/v1") {
		return nil
	}
	if st.SecretsBackend == "vault" {
		return nil // déjà migré (idempotent).
	}

	conn, err := client.DispenseFunction("secrets.kv/v1")
	if err != nil {
		return fmt.Errorf("migration des secrets vers %q : %w", m.Name, err)
	}
	kv := secretskvv1.NewSecretsKVClient(conn)

	entries, err := e.Secrets.List(ctx, "")
	if err != nil {
		return fmt.Errorf("migration des secrets : liste du backend file : %w", err)
	}

	migrated := 0
	for _, entry := range entries {
		if entry.Meta.Recovery {
			continue // docs06 : "Les entrées Recovery: true restent dans file".
		}
		value, err := e.Secrets.Get(ctx, entry.Ref)
		if err != nil {
			return fmt.Errorf("migration de %q : lecture file : %w", entry.Ref, err)
		}
		if _, err := kv.Write(ctx, &secretskvv1.WriteRequest{Ref: string(entry.Ref), Value: value.ExposeSecret()}); err != nil {
			return fmt.Errorf("migration de %q : écriture vault : %w", entry.Ref, err)
		}
		readBack, err := kv.Read(ctx, &secretskvv1.ReadRequest{Ref: string(entry.Ref)})
		if err != nil {
			return fmt.Errorf("migration de %q : relecture vault : %w", entry.Ref, err)
		}
		if !readBack.GetFound() || readBack.GetValue() != value.ExposeSecret() {
			return fmt.Errorf("migration de %q : relecture vault ne correspond pas à la valeur écrite", entry.Ref)
		}
		migrated++
	}

	st.SecretsBackend = "vault"
	if err := state.Save(e.StateDir, st); err != nil {
		return fmt.Errorf("persistance du backend de secrets après migration : %w", err)
	}
	e.audit(m.Name, "secrets.migrate", fmt.Sprintf("%d entrée(s) migrée(s) vers vault", migrated))
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
		return fmt.Errorf("module %q, étape %s : échec (%s)", m.Name, stepName, diagnosticsString(result.GetDiagnostics()))
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
			return nil, fmt.Errorf("état persisté invalide pour %q : %w", m.Name, err)
		}
		s, err := structpb.NewStruct(raw)
		if err != nil {
			return nil, fmt.Errorf("encodage de l'état de %q : %w", m.Name, err)
		}
		stateStruct = s
	}

	moduleConfig := m.Config
	if moduleConfig == nil {
		moduleConfig = map[string]any{}
	}
	resolvedConfig, err := resolveRefs(moduleConfig)
	if err != nil {
		return nil, fmt.Errorf("résolution des références de %q : %w", m.Name, err)
	}
	config, err := structpb.NewStruct(resolvedConfig)
	if err != nil {
		return nil, fmt.Errorf("encodage de la config de %q : %w", m.Name, err)
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
		return fmt.Errorf("encodage de l'état de %q : %w", moduleName, err)
	}
	st.Modules[moduleName] = state.ModuleState{StateJSON: raw}
	if err := state.Save(e.StateDir, st); err != nil {
		return fmt.Errorf("persistance de l'état après l'étape de %q : %w", moduleName, err)
	}
	return nil
}

func (e *Engine) audit(module, step, outcome string) {
	e.Logger.Info("étape", "module", module, "étape", step, "résultat", outcome)
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

// requiredFunctions retourne les fonctions déclarées en requires, telles
// quelles — suffixe @seed/@target inclus quand présent (docs/03 §1 : force
// le fournisseur) : la clé de registre correspondante n'existe que sous
// cette forme qualifiée (voir Run, enregistrement par phase).
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
		return "aucun diagnostic"
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
		return "", fmt.Errorf("génération de l'identifiant d'exécution : %w", err)
	}
	return "run-" + hex.EncodeToString(buf), nil
}
