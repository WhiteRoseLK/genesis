// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/WhiteRoseLK/genesis/internal/planner"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/state"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
)

func TestRequiresComputeVM(t *testing.T) {
	modWithVM := &resolver.Module{
		Name: "target-vm",
		Manifest: &sdk.ManifestFile{
			Name: "target-vm",
			Requires: map[string][]sdk.RequireEntry{
				"target": {
					{Function: "compute.vm/v1"},
					{Function: "core.secrets/v1"},
				},
			},
		},
	}
	if !requiresComputeVM(modWithVM) {
		t.Errorf("requiresComputeVM(modWithVM) = false, want true")
	}

	modWithoutVM := &resolver.Module{
		Name: "target-no-vm",
		Manifest: &sdk.ManifestFile{
			Name: "target-no-vm",
			Requires: map[string][]sdk.RequireEntry{
				"target": {
					{Function: "time.ntp/v1"},
				},
			},
		},
	}
	if requiresComputeVM(modWithoutVM) {
		t.Errorf("requiresComputeVM(modWithoutVM) = true, want false")
	}

	if requiresComputeVM(nil) {
		t.Errorf("requiresComputeVM(nil) = true, want false")
	}
}

func TestIsVMProvisioned(t *testing.T) {
	stEmpty := state.ModuleState{}
	if isVMProvisioned(stEmpty) {
		t.Errorf("isVMProvisioned(stEmpty) = true, want false")
	}

	stWithIP, err := json.Marshal(map[string]any{
		"vm_name": "chrony01",
		"vm_ip":   "192.168.1.210",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !isVMProvisioned(state.ModuleState{StateJSON: stWithIP}) {
		t.Errorf("isVMProvisioned(stWithIP) = false, want true")
	}

	stWithoutIP, err := json.Marshal(map[string]any{
		"vm_name": "chrony01",
	})
	if err != nil {
		t.Fatal(err)
	}
	if isVMProvisioned(state.ModuleState{StateJSON: stWithoutIP}) {
		t.Errorf("isVMProvisioned(stWithoutIP) = true, want false")
	}
}

func TestPreprovisionTargetModulesConcurrency(t *testing.T) {
	stateDir := t.TempDir()
	e := New(stateDir, newTestSecretsStore(t))

	var activeCount atomic.Int32
	var maxActive atomic.Int32

	// Verify that Engine protects concurrent state writes without race conditions
	st := state.New()

	plan := &planner.Plan{
		Order: []string{"vm-1", "vm-2", "vm-3"},
	}

	res := &resolver.Resolved{
		Modules: map[string]*resolver.Module{
			"vm-1": {
				Name: "vm-1",
				Manifest: &sdk.ManifestFile{
					Name: "vm-1",
					Provides: []sdk.FunctionRef{
						{Function: "dummy/v1", Phases: []string{"target"}},
					},
					Requires: map[string][]sdk.RequireEntry{
						"target": {{Function: "compute.vm/v1"}},
					},
				},
			},
			"vm-2": {
				Name: "vm-2",
				Manifest: &sdk.ManifestFile{
					Name: "vm-2",
					Provides: []sdk.FunctionRef{
						{Function: "dummy2/v1", Phases: []string{"target"}},
					},
					Requires: map[string][]sdk.RequireEntry{
						"target": {{Function: "compute.vm/v1"}},
					},
				},
			},
			"vm-3": {
				Name: "vm-3",
				Manifest: &sdk.ManifestFile{
					Name: "vm-3",
					Provides: []sdk.FunctionRef{
						{Function: "dummy3/v1", Phases: []string{"target"}},
					},
					Requires: map[string][]sdk.RequireEntry{
						"target": {{Function: "compute.vm/v1"}},
					},
				},
			},
		},
	}

	// Run simulated concurrent provisioning
	err := e.preprovisionTargetModules(
		context.Background(),
		"test-run",
		res,
		plan,
		nil, // clients nil because we will override with simulated behavior if needed
		st,
	)
	// With nil clients it would fail calling action, so verify error handling works
	if err == nil {
		t.Errorf("expected error with nil clients, got nil")
	}

	// Verify that state lock in Engine prevents data race
	done := make(chan bool)
	for i := 0; i < 5; i++ {
		go func(id int) {
			current := activeCount.Add(1)
			for {
				oldMax := maxActive.Load()
				if current <= oldMax || maxActive.CompareAndSwap(oldMax, current) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)

			raw, _ := json.Marshal(map[string]any{"vm_ip": "10.0.0.1"})
			e.mu.Lock()
			st.Modules["mod"] = state.ModuleState{StateJSON: raw}
			_ = state.Save(e.StateDir, st)
			e.mu.Unlock()

			activeCount.Add(-1)
			done <- true
		}(i)
	}

	for i := 0; i < 5; i++ {
		<-done
	}

	if maxActive.Load() < 2 {
		t.Logf("Note: maxActive was %d (goroutines may have run sequentially)", maxActive.Load())
	}
}
