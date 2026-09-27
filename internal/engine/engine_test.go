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

// buildAndInstall builds the source module under test/modules/<name> and
// installs it into searchRoot/<name>/<version>/ (the layout expected by
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
		t.Fatalf("building %s: %v\n%s", name, err, out)
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
	t.Fatalf("module %q not discovered after installation", name)
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

// TestRunExecutesChainInOrderAndSecondRunIsNoop covers two acceptance
// criteria of milestone M4 (doc 08) with the same mechanism: "kill then
// re-run -> resumption" (a new engine instance that reads the same state
// back does not redo the work already done — exactly what would happen
// after a kill+restart of the core) and "second apply -> 0 changes".
func TestRunExecutesChainInOrderAndSecondRunIsNoop(t *testing.T) {
	searchRoot := t.TempDir()
	installedA := buildAndInstall(t, "test-a", searchRoot)
	installedB := buildAndInstall(t, "test-b", searchRoot)
	installedC := buildAndInstall(t, "test-c", searchRoot)

	resolved, err := resolver.Resolve(testEnv(), []modulehost.Installed{installedA, installedB, installedC})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := plan.Order; indexOf(got, "test-a") >= indexOf(got, "test-b") || indexOf(got, "test-b") >= indexOf(got, "test-c") {
		t.Fatalf("plan order = %v, want test-a then test-b then test-c", got)
	}

	stateDir := t.TempDir()
	var firstLog bytes.Buffer

	e1 := New(stateDir, newTestSecretsStore(t))
	e1.Logger = slog.New(slog.NewTextHandler(&firstLog, nil))
	if err := e1.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if !strings.Contains(firstLog.String(), "step=verify") {
		t.Errorf("the first run should have executed verify at least once:\n%s", firstLog.String())
	}

	st, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("Load state after the first run: %v", err)
	}
	for _, name := range []string{"test-a", "test-b", "test-c"} {
		ms, ok := st.Modules[name]
		if !ok || len(ms.StateJSON) == 0 {
			t.Errorf("no persisted state for %q after the first run", name)
			continue
		}
		var flags map[string]any
		if err := json.Unmarshal(ms.StateJSON, &flags); err != nil {
			t.Fatalf("invalid state for %q: %v", name, err)
		}
		if v, _ := flags["verified"].(bool); !v {
			t.Errorf("%q: verified=%v, want true after the first run", name, flags["verified"])
		}
	}

	// Second run: a brand new engine instance, as after a kill+restart of
	// the core. It must find everything already compliant.
	var secondLog bytes.Buffer
	e2 := New(stateDir, newTestSecretsStore(t)) // different secret backend: irrelevant, unused here
	e2.Logger = slog.New(slog.NewTextHandler(&secondLog, nil))
	if err := e2.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	logStr := secondLog.String()
	for _, forbidden := range []string{"step=provision", "step=configure", "step=verify", "step=seed.up", "step=handover", "step=seed.retire"} {
		if strings.Contains(logStr, forbidden+" outcome=ok") {
			t.Errorf("the second run executed %q, want 0 changes:\n%s", forbidden, logStr)
		}
	}
	if !strings.Contains(logStr, "step=check outcome=\"compliant, no action\"") {
		t.Errorf("the second run should find every module compliant:\n%s", logStr)
	}
}

// TestRunResumesFromPersistedState checks that Run only executes what is
// missing when the persisted state shows that a module is already done —
// the concrete mechanism behind "kill then re-run -> resumption".
func TestRunResumesFromPersistedState(t *testing.T) {
	searchRoot := t.TempDir()
	installedA := buildAndInstall(t, "test-a", searchRoot)
	installedB := buildAndInstall(t, "test-b", searchRoot)
	installedC := buildAndInstall(t, "test-c", searchRoot)

	resolved, err := resolver.Resolve(testEnv(), []modulehost.Installed{installedA, installedB, installedC})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	stateDir := t.TempDir()
	// Pre-fills the state as if test-a had already finished (kill after
	// test-a, before test-b) during a previous run.
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
		t.Fatalf("Run: %v", err)
	}

	logStr := logBuf.String()
	if strings.Contains(logStr, "module=test-a step=provision outcome=ok") {
		t.Errorf("test-a was already done and must not be provisioned again:\n%s", logStr)
	}
	if !strings.Contains(logStr, "module=test-b step=verify outcome=ok") {
		t.Errorf("test-b should have run (not done yet):\n%s", logStr)
	}
	if !strings.Contains(logStr, "module=test-c step=verify outcome=ok") {
		t.Errorf("test-c should have run (not done yet):\n%s", logStr)
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
