// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusUninitialized(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "empty-state")
	out, err := runCLI(t, "status", "--state-dir", stateDir)
	if err == nil {
		t.Fatalf("status should fail when state is not initialized, got: %s", out)
	}
}

func TestStatusAfterInit(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	if out, err := runCLI(t, "init", "--state-dir", stateDir); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	out, err := runCLI(t, "status", "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Secrets backend: file") {
		t.Errorf("status stdout = %q, want Secrets backend: file", out)
	}
	if !strings.Contains(out, "Seed: active") {
		t.Errorf("status stdout = %q, want Seed: active", out)
	}
	if !strings.Contains(out, "(none)") {
		t.Errorf("status stdout = %q, want (none) for modules", out)
	}
}

func TestStatusWithSpec(t *testing.T) {
	useTempHomeKeepingGoCache(t)
	lockFile := filepath.Join(t.TempDir(), "genesis.lock")
	installChainModules(t, lockFile)

	stateDir := filepath.Join(t.TempDir(), "state")
	if out, err := runCLI(t, "init", "--state-dir", stateDir); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	specFile := writeChainSpec(t)
	out, err := runCLI(t, "status", "--state-dir", stateDir, "-f", specFile)
	if err != nil {
		t.Fatalf("status with spec: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Functions (active providers):") {
		t.Errorf("status with spec = %q, want Functions section", out)
	}
}
