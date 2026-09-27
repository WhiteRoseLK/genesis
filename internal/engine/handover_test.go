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

// TestHandoverRetiresCrossModuleSeedProvider proves the cross-module
// handover between TWO distinct modules (docs/05-bootstrap-lifecycle.md,
// milestone M6, doc 08 criterion "after the handover, stopping [the seed
// module] has no impact") — unlike test-a (which takes over from itself),
// test-e (pure seed) and test-f (target, reads test.e/v1@seed) are two
// separate processes: this test checks that test-f takes over test.e/v1
// (Handover really reads test.e/v1@seed), then that the test-e seed is only
// stopped at the end of Run, after the final verification (ADR-020).
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
		t.Fatalf("Resolve: %v", err)
	}
	if _, ok := resolved.Modules["test-e"]; !ok {
		t.Fatal("test-e should have been added automatically for test.e/v1@seed")
	}
	if got := resolved.CapabilityModule["test-e"]; got != "" {
		t.Errorf("test-e, added automatically, should carry no explicit capability, got %q", got)
	}

	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if indexOf(plan.Order, "test-e") >= indexOf(plan.Order, "test-f") {
		t.Fatalf("plan order = %v, want test-e before test-f", plan.Order)
	}

	stateDir := t.TempDir()
	var logBuf bytes.Buffer
	e := New(stateDir, newTestSecretsStore(t))
	e.Logger = slog.New(slog.NewTextHandler(&logBuf, nil))
	if err := e.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}

	logStr := logBuf.String()
	if !strings.Contains(logStr, "module=test-e step=seed.up outcome=ok") {
		t.Errorf("test-e should have received SeedUp:\n%s", logStr)
	}
	if !strings.Contains(logStr, "module=test-f step=handover outcome=ok") {
		t.Errorf("test-f should have received Handover:\n%s", logStr)
	}
	// SeedDown targets the test-e seed, never the test-f target module.
	if !strings.Contains(logStr, "module=test-e step=seed.retire outcome=ok") {
		t.Errorf("test-e should have received SeedDown after test-f's handover:\n%s", logStr)
	}
	if strings.Contains(logStr, "module=test-f step=seed.retire") {
		t.Errorf("test-f (target module, not seed) should never have received SeedDown:\n%s", logStr)
	}
	// test-e provides nothing in the target phase: its own plan iteration
	// must never attempt provision/configure/handover.
	for _, forbidden := range []string{"module=test-e step=provision", "module=test-e step=configure", "module=test-e step=handover"} {
		if strings.Contains(logStr, forbidden) {
			t.Errorf("test-e (pure seed) should never have received %q:\n%s", forbidden, logStr)
		}
	}

	// The seed stays active during the whole build of the target: SeedDown
	// only happens after the handover AND the final verification (ADR-020).
	handoverAt := strings.Index(logStr, "module=test-f step=handover outcome=ok")
	finalVerifyAt := strings.Index(logStr, "module=test-f step=verify.final outcome=ok")
	retireAt := strings.Index(logStr, "module=test-e step=seed.retire outcome=ok")
	if handoverAt < 0 || finalVerifyAt < 0 || retireAt < 0 || handoverAt >= finalVerifyAt || finalVerifyAt >= retireAt {
		t.Errorf("expected order: handover(test-f) < verify.final(test-f) < seed.retire(test-e):\n%s", logStr)
	}

	st, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("Load state: %v", err)
	}
	if !st.SeedRetired {
		t.Error("state: seed_retired=false, want true after the seed retirement")
	}
	assertFlag(t, st, "test-e", "retired", true)
	assertFlag(t, st, "test-f", "handed_over", true)

	var fFlags map[string]any
	if err := json.Unmarshal(st.Modules["test-f"].StateJSON, &fFlags); err != nil {
		t.Fatalf("invalid test-f state: %v", err)
	}
	if got := fFlags["handover_source"]; got != "test-e" {
		t.Errorf("test-f state handover_source = %v, want \"test-e\" (proof of a real read of test.e/v1@seed)", got)
	}

	// Second run: compliant everywhere, nothing is replayed (idempotence of
	// the handover itself, docs/08-milestones.md).
	var secondLog bytes.Buffer
	e2 := New(stateDir, newTestSecretsStore(t))
	e2.Logger = slog.New(slog.NewTextHandler(&secondLog, nil))
	if err := e2.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	secondLogStr := secondLog.String()
	if !strings.Contains(secondLogStr, "module=test-e step=check outcome=\"seed retired, skipped\"") {
		t.Errorf("after retirement, the seed must not even be queried any more:\n%s", secondLogStr)
	}
	for _, forbidden := range []string{"step=seed.up", "step=provision", "step=configure", "step=verify", "step=handover", "step=seed.retire"} {
		if strings.Contains(secondLogStr, forbidden+" outcome=ok") {
			t.Errorf("the second run executed %q, want 0 changes:\n%s", forbidden, secondLogStr)
		}
	}
}

func assertFlag(t *testing.T, st *state.State, module, flag string, want bool) {
	t.Helper()
	ms, ok := st.Modules[module]
	if !ok {
		t.Fatalf("no persisted state for %q", module)
	}
	var flags map[string]any
	if err := json.Unmarshal(ms.StateJSON, &flags); err != nil {
		t.Fatalf("invalid state for %q: %v", module, err)
	}
	got, _ := flags[flag].(bool)
	if got != want {
		t.Errorf("%q: %s=%v, want %v", module, flag, got, want)
	}
}

// TestSeedKeptWhenAFunctionHasNoTarget: a seed function with no target
// successor (test-e alone, nobody takes over test.e/v1) forbids the
// retirement — the seed remains the only provider.
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
		t.Fatalf("Resolve: %v", err)
	}
	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	stateDir := t.TempDir()
	var logBuf bytes.Buffer
	e := New(stateDir, newTestSecretsStore(t))
	e.Logger = slog.New(slog.NewTextHandler(&logBuf, nil))
	if err := e.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}

	logStr := logBuf.String()
	if strings.Contains(logStr, "step=seed.retire outcome=ok") {
		t.Errorf("the seed must not be retired without a target successor:\n%s", logStr)
	}
	if !strings.Contains(logStr, "no target successor for test.e/v1") {
		t.Errorf("the log should explain why the seed is kept:\n%s", logStr)
	}
	st, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("Load state: %v", err)
	}
	if st.SeedRetired {
		t.Error("state: seed_retired=true, want false")
	}
}
