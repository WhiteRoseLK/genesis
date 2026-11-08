// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/state"
)

// #152: a failure while cleaning the verifier VMs must be reported, the local
// cleanup must still run, and the command must not claim success.
func TestCleanReportsVerifierVMFailure(t *testing.T) {
	t.Setenv("GENESIS_MODULE_PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	stateDir := filepath.Join(t.TempDir(), "state")
	if _, err := runCLI(t, "init", "--state-dir", stateDir); err != nil {
		t.Fatalf("init: %v", err)
	}
	leftover := filepath.Join(stateDir, "secrets", "dummy")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	specFile := writeStateDirSpec(t, stateDir)

	out, errOut, err := runCLISplit(t, "clean", "-f", specFile, "--auto-approve")
	if err == nil {
		t.Fatalf("clean succeeded although the modules are not installed\n%s", out)
	}
	if !strings.Contains(err.Error(), "clean incomplete") {
		t.Errorf("error = %q, want it to start with 'clean incomplete'", err)
	}
	if !strings.Contains(errOut, "verifier VMs") || !strings.Contains(errOut, "genesis modules install") {
		t.Errorf("stderr = %q, want the failed step and a hint", errOut)
	}
	if strings.Contains(out, "clean complete") || strings.Contains(out, "cleaned cluster verifier VMs") {
		t.Errorf("stdout = %q, must not claim success", out)
	}
	if _, statErr := os.Stat(leftover); !os.IsNotExist(statErr) {
		t.Errorf("local cleanup did not run after the failure: %v", statErr)
	}
}

func TestCleanReportsSecretsStoreFailure(t *testing.T) {
	t.Setenv("GENESIS_MODULE_PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	// No `genesis init`: the master key is missing.
	stateDir := filepath.Join(t.TempDir(), "state")
	specFile := writeStateDirSpec(t, stateDir)

	out, _, err := runCLISplit(t, "clean", "-f", specFile, "--auto-approve")
	if err == nil || !strings.Contains(err.Error(), "clean incomplete") {
		t.Fatalf("err = %v, want 'clean incomplete'\n%s", err, out)
	}
	if strings.Contains(out, "clean complete") {
		t.Errorf("stdout = %q, must not print 'clean complete'", out)
	}
}

func TestCleanCommand(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	if _, err := runCLI(t, "init", "--state-dir", stateDir); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Create some dummy intermediate secrets
	secretsDir := filepath.Join(stateDir, "secrets", "dummy")
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secretsDir, "secret.age"), []byte("encrypted"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Set state as retired
	st, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	st.SeedRetired = true
	st.SecretsBackend = "vault"
	if err := state.Save(stateDir, st); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	// Run clean
	out, err := runCLI(t, "clean", "--auto-approve", "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("clean: %v\n%s", err, out)
	}
	if !strings.Contains(out, "clean complete") {
		t.Errorf("clean output = %q, want 'clean complete'", out)
	}

	// Verify secrets were removed
	entries, err := os.ReadDir(filepath.Join(stateDir, "secrets"))
	if err != nil {
		t.Fatalf("ReadDir secrets: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected secrets dir to be empty, found %d entries", len(entries))
	}

	// Verify master key is preserved
	if _, err := os.Stat(filepath.Join(stateDir, "master.key")); err != nil {
		t.Errorf("master.key should be preserved: %v", err)
	}

	// Verify state is reset
	stAfter, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("state.Load after clean: %v", err)
	}
	if stAfter.SeedRetired {
		t.Errorf("stAfter.SeedRetired = true, want false")
	}
	if stAfter.SecretsBackend != "file" {
		t.Errorf("stAfter.SecretsBackend = %q, want 'file'", stAfter.SecretsBackend)
	}
}
