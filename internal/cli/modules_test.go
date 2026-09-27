// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// sourceModuleDir locates test/modules/panicking, used as a real,
// buildable module fixture for install/list/verify tests.
func sourceModuleDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..", "test", "modules", "panicking")
}

// useTempHomeKeepingGoCache fakes $HOME (so DefaultInstallDir writes under a
// throwaway directory) while keeping the real GOMODCACHE/GOCACHE, so `go
// build` invoked by `modules install` reuses the already-warm module cache
// instead of redownloading everything into a doomed-to-fail-cleanup temp dir
// (the Go module cache is read-only by design).
func useTempHomeKeepingGoCache(t *testing.T) (home string) {
	t.Helper()

	// Read the real values BEFORE faking $HOME: `go env` derives its defaults
	// from it (GOPATH=$HOME/go...), so querying them afterwards would simply
	// return the fake paths.
	real := map[string]string{}
	for _, name := range []string{"GOMODCACHE", "GOCACHE", "GOPATH"} {
		out, err := exec.Command("go", "env", name).Output()
		if err != nil {
			t.Fatalf("go env %s: %v", name, err)
		}
		real[name] = strings.TrimSpace(string(out))
	}

	home = t.TempDir()
	t.Setenv("HOME", home)
	for name, value := range real {
		t.Setenv(name, value)
	}
	return home
}

func TestModulesInstallListVerify(t *testing.T) {
	useTempHomeKeepingGoCache(t)
	t.Setenv("GENESIS_MODULE_PATH", "")
	lockFile := filepath.Join(t.TempDir(), "genesis.lock")

	installOut, err := runCLI(t, "modules", "install", sourceModuleDir(t), "--lock-file", lockFile)
	if err != nil {
		t.Fatalf("modules install: %v\n%s", err, installOut)
	}
	if !strings.Contains(installOut, "panicking@0.1.0") {
		t.Errorf("install output = %q, want panicking@0.1.0 mentioned", installOut)
	}
	if _, err := os.Stat(lockFile); err != nil {
		t.Errorf("genesis.lock missing after install: %v", err)
	}

	listOut, err := runCLI(t, "modules", "list")
	if err != nil {
		t.Fatalf("modules list: %v", err)
	}
	if !strings.Contains(listOut, "panicking") {
		t.Errorf("modules list = %q, want panicking", listOut)
	}

	verifyOut, err := runCLI(t, "modules", "verify", "--lock-file", lockFile)
	if err != nil {
		t.Fatalf("modules verify: %v\n%s", err, verifyOut)
	}
	if !strings.Contains(verifyOut, "1 module(s) verified") {
		t.Errorf("modules verify = %q, want confirmation of 1 verified module", verifyOut)
	}
}

// TestModulesVerifyRejectsTamperedBinary is the M3 acceptance criterion (doc
// 08): a module whose digest differs from the lock is refused.
func TestModulesVerifyRejectsTamperedBinary(t *testing.T) {
	home := useTempHomeKeepingGoCache(t)
	t.Setenv("GENESIS_MODULE_PATH", "")
	lockFile := filepath.Join(t.TempDir(), "genesis.lock")

	if _, err := runCLI(t, "modules", "install", sourceModuleDir(t), "--lock-file", lockFile); err != nil {
		t.Fatalf("modules install: %v", err)
	}

	binaryName := fmt.Sprintf("module-%s-%s", runtime.GOOS, runtime.GOARCH)
	binaryPath := filepath.Join(home, ".local", "share", "genesis", "modules", "panicking", "0.1.0", binaryName)
	f, err := os.OpenFile(binaryPath, os.O_APPEND|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatalf("opening the installed binary: %v", err)
	}
	if _, err := f.WriteString("tampered"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := runCLI(t, "modules", "verify", "--lock-file", lockFile); err == nil {
		t.Fatal("modules verify on a modified binary: unexpected success, should have been refused")
	}
}
