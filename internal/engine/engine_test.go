// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/planner"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/secrets"
	"github.com/WhiteRoseLK/genesis/internal/spec"
	"github.com/WhiteRoseLK/genesis/internal/state"
)

// buildAndInstall compile le module source sous test/modules/<name> et
// l'installe dans searchRoot/<name>/<version>/ (layout attendu par
// internal/modulehost.Discover, docs/02-architecture.md).
func buildAndInstall(t *testing.T, name, searchRoot string) modulehost.Installed {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(wd, "..", "..", "test", "modules", name)

	destDir := filepath.Join(searchRoot, name, "0.1.0")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}

	binaryPath := filepath.Join(destDir, modulehost.BinaryName())
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = sourceDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compilation de %s : %v\n%s", name, err, out)
	}

	moduleYAML, err := os.ReadFile(filepath.Join(sourceDir, "module.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "module.yaml"), moduleYAML, 0o644); err != nil {
		t.Fatal(err)
	}

	found, err := modulehost.Discover([]string{searchRoot})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range found {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("module %q non découvert après installation", name)
	return modulehost.Installed{}
}

func testEnv() *spec.Environment {
	return &spec.Environment{
		Capabilities: map[string]spec.Capability{
			"test-a": {Module: "test-a"},
			"test-b": {Module: "test-b"},
			"test-c": {Module: "test-c"},
		},
	}
}

func newTestSecretsStore(t *testing.T) secrets.Store {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	return secrets.NewFileStore(t.TempDir(), identity)
}

// TestRunExecutesChainInOrderAndSecondRunIsNoop couvre deux critères
// d'acceptation du jalon J4 (doc 08) avec le même mécanisme : "kill puis
// relance -> reprise" (une nouvelle instance de moteur qui relit le même
// état ne refait pas le travail déjà fait — c'est exactement ce qui se
// passerait après un kill+relance du cœur) et "second apply -> 0 changement".
func TestRunExecutesChainInOrderAndSecondRunIsNoop(t *testing.T) {
	searchRoot := t.TempDir()
	installedA := buildAndInstall(t, "test-a", searchRoot)
	installedB := buildAndInstall(t, "test-b", searchRoot)
	installedC := buildAndInstall(t, "test-c", searchRoot)

	resolved, err := resolver.Resolve(testEnv(), []modulehost.Installed{installedA, installedB, installedC})
	if err != nil {
		t.Fatalf("Resolve : %v", err)
	}
	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("Build : %v", err)
	}
	if got := plan.Order; indexOf(got, "test-a") >= indexOf(got, "test-b") || indexOf(got, "test-b") >= indexOf(got, "test-c") {
		t.Fatalf("ordre du plan = %v, attendu test-a puis test-b puis test-c", got)
	}

	stateDir := t.TempDir()
	var firstLog bytes.Buffer

	e1 := New(stateDir, newTestSecretsStore(t))
	e1.Logger = slog.New(slog.NewTextHandler(&firstLog, nil))
	if err := e1.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("premier Run : %v", err)
	}
	if !strings.Contains(firstLog.String(), "étape=verify") {
		t.Errorf("le premier run devrait avoir exécuté verify au moins une fois :\n%s", firstLog.String())
	}

	st, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("Load état après le premier run : %v", err)
	}
	for _, name := range []string{"test-a", "test-b", "test-c"} {
		ms, ok := st.Modules[name]
		if !ok || len(ms.StateJSON) == 0 {
			t.Errorf("aucun état persisté pour %q après le premier run", name)
			continue
		}
		var flags map[string]any
		if err := json.Unmarshal(ms.StateJSON, &flags); err != nil {
			t.Fatalf("état invalide pour %q : %v", name, err)
		}
		if v, _ := flags["verified"].(bool); !v {
			t.Errorf("%q : verified=%v, attendu true après le premier run", name, flags["verified"])
		}
	}

	// Seconde exécution : une toute nouvelle instance de moteur, comme après
	// un kill+relance du cœur. Elle doit tout retrouver déjà conforme.
	var secondLog bytes.Buffer
	e2 := New(stateDir, newTestSecretsStore(t)) // backend secrets différent : sans importance, non utilisé ici
	e2.Logger = slog.New(slog.NewTextHandler(&secondLog, nil))
	if err := e2.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("second Run : %v", err)
	}

	logStr := secondLog.String()
	for _, forbidden := range []string{"étape=provision", "étape=configure", "étape=verify", "étape=seed.up", "étape=handover", "étape=seed.retire"} {
		if strings.Contains(logStr, forbidden+" résultat=ok") {
			t.Errorf("le second run a exécuté %q, attendu 0 changement :\n%s", forbidden, logStr)
		}
	}
	if !strings.Contains(logStr, "étape=check résultat=\"conforme, aucune action\"") {
		t.Errorf("le second run devrait constater conforme pour chaque module :\n%s", logStr)
	}
}

// TestRunResumesFromPersistedState vérifie que Run n'exécute que ce qui
// manque quand l'état persisté montre qu'un module est déjà terminé — le
// mécanisme concret derrière "kill puis relance -> reprise".
func TestRunResumesFromPersistedState(t *testing.T) {
	searchRoot := t.TempDir()
	installedA := buildAndInstall(t, "test-a", searchRoot)
	installedB := buildAndInstall(t, "test-b", searchRoot)
	installedC := buildAndInstall(t, "test-c", searchRoot)

	resolved, err := resolver.Resolve(testEnv(), []modulehost.Installed{installedA, installedB, installedC})
	if err != nil {
		t.Fatalf("Resolve : %v", err)
	}
	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("Build : %v", err)
	}

	stateDir := t.TempDir()
	// Pré-remplit l'état comme si test-a avait déjà terminé (kill après
	// test-a, avant test-b) lors d'une exécution précédente.
	pre := state.New()
	doneA, err := json.Marshal(map[string]any{
		"seeded": true, "provisioned": true, "configured": true,
		"verified": true, "handed_over": true, "seed_down": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	pre.Modules["test-a"] = state.ModuleState{StateJSON: doneA}
	if err := state.Save(stateDir, pre); err != nil {
		t.Fatal(err)
	}

	var logBuf bytes.Buffer
	e := New(stateDir, newTestSecretsStore(t))
	e.Logger = slog.New(slog.NewTextHandler(&logBuf, nil))
	if err := e.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("Run : %v", err)
	}

	logStr := logBuf.String()
	if strings.Contains(logStr, "module=test-a étape=provision résultat=ok") {
		t.Errorf("test-a était déjà terminé, ne devait pas être reprovisionné :\n%s", logStr)
	}
	if !strings.Contains(logStr, "module=test-b étape=verify résultat=ok") {
		t.Errorf("test-b devait être exécuté (pas encore fait) :\n%s", logStr)
	}
	if !strings.Contains(logStr, "module=test-c étape=verify résultat=ok") {
		t.Errorf("test-c devait être exécuté (pas encore fait) :\n%s", logStr)
	}
}

func indexOf(order []string, name string) int {
	for i, n := range order {
		if n == name {
			return i
		}
	}
	return -1
}
