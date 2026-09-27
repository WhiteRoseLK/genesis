// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/planner"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/spec"
)

// TestAddingTestDInsertsAtCorrectPlanPosition is the last acceptance
// criterion of milestone M4 (doc 08): "adding a test-d module that depends
// on test-a, without changing any file outside test/modules/test-d/ →
// inserted at the right place in the plan."
//
// test-d already exists as a fixture under test/modules/test-d/ (like
// test-a/b/c); this test proves that the resolver, the planner and the
// engine needed no change to support it — no file in this package knows
// "test-d" by name.
func TestAddingTestDInsertsAtCorrectPlanPosition(t *testing.T) {
	searchRoot := t.TempDir()
	installedA := buildAndInstall(t, "test-a", searchRoot)
	installedB := buildAndInstall(t, "test-b", searchRoot)
	installedC := buildAndInstall(t, "test-c", searchRoot)
	installedD := buildAndInstall(t, "test-d", searchRoot)

	env := testEnv()
	env.Capabilities["test-d"] = spec.Capability{Module: "test-d"}

	resolved, err := resolver.Resolve(env, []modulehost.Installed{installedA, installedB, installedC, installedD})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(plan.Order) != 4 {
		t.Fatalf("Order = %v, want 4 modules", plan.Order)
	}
	if indexOf(plan.Order, "test-a") >= indexOf(plan.Order, "test-d") {
		t.Errorf("test-a should come before test-d: %v", plan.Order)
	}
	// test-d depends neither on test-b nor on test-c, and nothing depends on
	// it: its position relative to them is not constrained, only its
	// position after test-a matters.

	stateDir := t.TempDir()
	e := New(stateDir, newTestSecretsStore(t))
	if err := e.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("Run with test-d: %v", err)
	}
}
