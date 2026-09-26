// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/planner"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/spec"
	"github.com/WhiteRoseLK/genesis/internal/state"
)

// TestHandoverRetiresCrossModuleSeedProvider prouve la passation croisée
// entre DEUX modules distincts (docs/05-cycle-bootstrap.md, jalon J6,
// critère du doc 08 "après passation, arrêt [du module graine] sans
// impact") — contrairement à test-a (qui se reprend lui-même), test-e
// (graine pure) et test-f (cible, lit test.e/v1@seed) sont deux processus
// séparés : ce test vérifie que test-f reprend test.e/v1 (Handover lit
// réellement test.e/v1@seed), puis que la graine test-e n'est arrêtée
// qu'en fin de Run, après la vérification finale (ADR-020).
func TestHandoverRetiresCrossModuleSeedProvider(t *testing.T) {
	searchRoot := t.TempDir()
	installedE := buildAndInstall(t, "test-e", searchRoot)
	installedF := buildAndInstall(t, "test-f", searchRoot)

	env := &spec.Environment{
		Capabilities: map[string]spec.Capability{
			"test-f": {Module: "test-f"},
		},
	}
	resolved, err := resolver.Resolve(env, []modulehost.Installed{installedE, installedF})
	if err != nil {
		t.Fatalf("Resolve : %v", err)
	}
	if _, ok := resolved.Modules["test-e"]; !ok {
		t.Fatal("test-e aurait dû être ajouté automatiquement pour test.e/v1@seed")
	}
	if got := resolved.CapabilityModule["test-e"]; got != "" {
		t.Errorf("test-e ajouté automatiquement ne devrait porter aucune capacité explicite, got %q", got)
	}

	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("Build : %v", err)
	}
	if indexOf(plan.Order, "test-e") >= indexOf(plan.Order, "test-f") {
		t.Fatalf("ordre du plan = %v, attendu test-e avant test-f", plan.Order)
	}

	stateDir := t.TempDir()
	var logBuf bytes.Buffer
	e := New(stateDir, newTestSecretsStore(t))
	e.Logger = slog.New(slog.NewTextHandler(&logBuf, nil))
	if err := e.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("Run : %v", err)
	}

	logStr := logBuf.String()
	if !strings.Contains(logStr, "module=test-e étape=seed.up résultat=ok") {
		t.Errorf("test-e aurait dû recevoir SeedUp :\n%s", logStr)
	}
	if !strings.Contains(logStr, "module=test-f étape=handover résultat=ok") {
		t.Errorf("test-f aurait dû recevoir Handover :\n%s", logStr)
	}
	// SeedDown vise la graine test-e, jamais le module cible test-f.
	if !strings.Contains(logStr, "module=test-e étape=seed.retire résultat=ok") {
		t.Errorf("test-e aurait dû recevoir SeedDown suite à la passation de test-f :\n%s", logStr)
	}
	if strings.Contains(logStr, "module=test-f étape=seed.retire") {
		t.Errorf("test-f (module cible, pas graine) n'aurait jamais dû recevoir SeedDown :\n%s", logStr)
	}
	// test-e ne fournit rien en phase cible : sa propre itération de plan
	// ne doit jamais tenter provision/configure/handover.
	for _, forbidden := range []string{"module=test-e étape=provision", "module=test-e étape=configure", "module=test-e étape=handover"} {
		if strings.Contains(logStr, forbidden) {
			t.Errorf("test-e (graine pure) n'aurait jamais dû recevoir %q :\n%s", forbidden, logStr)
		}
	}

	// La graine reste active pendant toute la construction de la cible :
	// SeedDown n'arrive qu'après la passation ET la vérification finale
	// (ADR-020).
	handoverAt := strings.Index(logStr, "module=test-f étape=handover résultat=ok")
	finalVerifyAt := strings.Index(logStr, "module=test-f étape=verify.final résultat=ok")
	retireAt := strings.Index(logStr, "module=test-e étape=seed.retire résultat=ok")
	if handoverAt < 0 || finalVerifyAt < 0 || retireAt < 0 || handoverAt >= finalVerifyAt || finalVerifyAt >= retireAt {
		t.Errorf("ordre attendu : handover(test-f) < verify.final(test-f) < seed.retire(test-e) :\n%s", logStr)
	}

	st, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("Load état : %v", err)
	}
	if !st.SeedRetired {
		t.Error("état : seed_retired=false, attendu true après retrait de la graine")
	}
	assertFlag(t, st, "test-e", "retired", true)
	assertFlag(t, st, "test-f", "handed_over", true)

	var fFlags map[string]any
	if err := json.Unmarshal(st.Modules["test-f"].StateJSON, &fFlags); err != nil {
		t.Fatalf("état de test-f invalide : %v", err)
	}
	if got := fFlags["handover_source"]; got != "test-e" {
		t.Errorf("test-f état handover_source = %v, attendu \"test-e\" (preuve d'une lecture réelle de test.e/v1@seed)", got)
	}

	// Second run : conforme partout, rien ne se rejoue (idempotence de la
	// passation elle-même, docs/08-jalons.md).
	var secondLog bytes.Buffer
	e2 := New(stateDir, newTestSecretsStore(t))
	e2.Logger = slog.New(slog.NewTextHandler(&secondLog, nil))
	if err := e2.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("second Run : %v", err)
	}
	secondLogStr := secondLog.String()
	if !strings.Contains(secondLogStr, "module=test-e étape=check résultat=\"graine retirée, ignoré\"") {
		t.Errorf("après retrait, la graine ne doit même plus être interrogée :\n%s", secondLogStr)
	}
	for _, forbidden := range []string{"étape=seed.up", "étape=provision", "étape=configure", "étape=verify", "étape=handover", "étape=seed.retire"} {
		if strings.Contains(secondLogStr, forbidden+" résultat=ok") {
			t.Errorf("le second run a exécuté %q, attendu 0 changement :\n%s", forbidden, secondLogStr)
		}
	}
}

func assertFlag(t *testing.T, st *state.State, module, flag string, want bool) {
	t.Helper()
	ms, ok := st.Modules[module]
	if !ok {
		t.Fatalf("aucun état persisté pour %q", module)
	}
	var flags map[string]any
	if err := json.Unmarshal(ms.StateJSON, &flags); err != nil {
		t.Fatalf("état invalide pour %q : %v", module, err)
	}
	got, _ := flags[flag].(bool)
	if got != want {
		t.Errorf("%q : %s=%v, attendu %v", module, flag, got, want)
	}
}

// TestSeedKeptWhenAFunctionHasNoTarget : une fonction graine sans relève
// cible (test-e seul, personne ne reprend test.e/v1) interdit le retrait —
// la graine reste l'unique fournisseur.
func TestSeedKeptWhenAFunctionHasNoTarget(t *testing.T) {
	searchRoot := t.TempDir()
	installedE := buildAndInstall(t, "test-e", searchRoot)

	env := &spec.Environment{
		Capabilities: map[string]spec.Capability{
			"test-e": {Module: "test-e"},
		},
	}
	resolved, err := resolver.Resolve(env, []modulehost.Installed{installedE})
	if err != nil {
		t.Fatalf("Resolve : %v", err)
	}
	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("Build : %v", err)
	}

	stateDir := t.TempDir()
	var logBuf bytes.Buffer
	e := New(stateDir, newTestSecretsStore(t))
	e.Logger = slog.New(slog.NewTextHandler(&logBuf, nil))
	if err := e.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("Run : %v", err)
	}

	logStr := logBuf.String()
	if strings.Contains(logStr, "étape=seed.retire résultat=ok") {
		t.Errorf("la graine ne devait pas être retirée sans relève cible :\n%s", logStr)
	}
	if !strings.Contains(logStr, "aucune relève cible pour test.e/v1") {
		t.Errorf("le journal devrait expliquer pourquoi la graine est conservée :\n%s", logStr)
	}
	st, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("Load état : %v", err)
	}
	if st.SeedRetired {
		t.Error("état : seed_retired=true, attendu false")
	}
}
