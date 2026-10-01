// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/state"
)

func TestDestroyDeletesTargetResources(t *testing.T) {
	useTempHomeKeepingGoCache(t)
	lockFile := filepath.Join(t.TempDir(), "genesis.lock")
	installChainModules(t, lockFile)

	stateDir := filepath.Join(t.TempDir(), "state")
	if _, err := runCLI(t, "init", "--state-dir", stateDir); err != nil {
		t.Fatalf("init: %v", err)
	}

	specFile := writeChainSpec(t)
	if out, err := runCLI(t, "apply", "-f", specFile, "--auto-approve", "--state-dir", stateDir); err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}

	st, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if len(st.Modules) == 0 {
		t.Fatal("modules should exist in state after apply")
	}

	out, err := runCLI(t, "destroy", "-f", specFile, "--auto-approve", "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("destroy: %v\n%s", err, out)
	}
	if !strings.Contains(out, "destroy complete") {
		t.Errorf("destroy output = %q, want 'destroy complete'", out)
	}

	stAfter, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("state.Load after destroy: %v", err)
	}
	if len(stAfter.Modules) != 0 {
		t.Errorf("modules after destroy = %v, want empty", stAfter.Modules)
	}
}
