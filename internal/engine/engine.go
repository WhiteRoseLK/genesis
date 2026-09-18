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
// La choreographie complète de passation multi-module du doc 05 (Repoint
// explicite des consommateurs) est différée : dans cette architecture, un
// consommateur redemande une session de broker à chaque appel, donc changer
// le fournisseur actif dans le registre suffit à "repointer" — rien de plus
// n'est nécessaire pour J4.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	modulev1 "genesis/sdk/go/gen/module/v1"
)

// Engine exécute un plan résolu.
type Engine struct {
	Registry *broker.Registry
	StateDir string
	Logger   *slog.Logger
}

// New construit un moteur : registre du broker avec core.secrets/v1 déjà
// enregistrée (fonction native, jamais un module).
func New(stateDir string, secretsStore secrets.Store) *Engine {
	registry := broker.NewRegistry()
	registry.SetNative("core.secrets/v1", broker.NativeSecrets(secretsStore))
	return &Engine{Registry: registry, StateDir: stateDir, Logger: slog.Default()}
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
			e.Registry.SetModuleProvider(p.Function, conn, register)
		}
	}

	runID, err := newRunID()
	if err != nil {
		return err
	}

	for _, name := range plan.Order {
		m := resolved.Modules[name]
		if err := e.runModule(ctx, runID, m, clients[name], st); err != nil {
			return err
		}
	}

	return nil
}

func (e *Engine) runModule(ctx context.Context, runID string, m *resolver.Module, client *modulehost.Client, st *state.State) error {
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

	hasSeed := providesInPhase(m, "seed")

	if hasSeed {
		if err := e.action(ctx, runID, m, client, st, "seed.up", client.Module().SeedUp); err != nil {
			return err
		}
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
	if hasSeed {
		if err := e.action(ctx, runID, m, client, st, "handover", client.Module().Handover); err != nil {
			return err
		}
		if err := e.action(ctx, runID, m, client, st, "seed.retire", client.Module().SeedDown); err != nil {
			return err
		}
	}
	return nil
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

func requiredFunctions(m *sdk.ManifestFile) []string {
	seen := map[string]bool{}
	var out []string
	for _, entries := range m.Requires {
		for _, entry := range entries {
			name, _ := resolver.SplitFunctionPhase(entry.Function)
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
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
