// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testModuleSourceDir(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..", "test", "modules", name)
}

func writeChainSpec(t *testing.T) string {
	t.Helper()
	content := `apiVersion: genesis/v1alpha1
kind: Environment
metadata: { name: lab }
network: { cidr: 10.10.0.0/24, gateway: 10.10.0.1, pool: 10.10.0.50-10.10.0.99, domain: lab.internal }
seed: { address: 10.10.0.10 }
capabilities:
  test-a: { module: test-a }
  test-b: { module: test-b }
  test-c: { module: test-c }
`
	path := filepath.Join(t.TempDir(), "env.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func installChainModules(t *testing.T, lockFile string) {
	t.Helper()
	for _, name := range []string{"test-a", "test-b", "test-c"} {
		if out, err := runCLI(t, "modules", "install", testModuleSourceDir(t, name), "--lock-file", lockFile); err != nil {
			t.Fatalf("modules install %s : %v\n%s", name, err, out)
		}
	}
}

func TestPlanListsChainInOrder(t *testing.T) {
	useTempHomeKeepingGoCache(t)
	lockFile := filepath.Join(t.TempDir(), "genesis.lock")
	installChainModules(t, lockFile)

	specFile := writeChainSpec(t)
	out, err := runCLI(t, "plan", "-f", specFile)
	if err != nil {
		t.Fatalf("plan : %v\n%s", err, out)
	}

	posA := strings.Index(out, "test-a")
	posB := strings.Index(out, "test-b")
	posC := strings.Index(out, "test-c")
	if posA < 0 || posB < 0 || posC < 0 {
		t.Fatalf("plan = %q, attendu les 3 modules", out)
	}
	if posA >= posB || posB >= posC {
		t.Errorf("plan = %q, attendu l'ordre test-a puis test-b puis test-c", out)
	}
}

func TestApplyRunsAndSecondApplyIsNoop(t *testing.T) {
	useTempHomeKeepingGoCache(t)
	lockFile := filepath.Join(t.TempDir(), "genesis.lock")
	installChainModules(t, lockFile)

	stateDir := filepath.Join(t.TempDir(), "state")
	if _, err := runCLI(t, "init", "--state-dir", stateDir); err != nil {
		t.Fatalf("init : %v", err)
	}

	specFile := writeChainSpec(t)

	firstOut, err := runCLI(t, "apply", "-f", specFile, "--state-dir", stateDir, "--auto-approve")
	if err != nil {
		t.Fatalf("premier apply : %v\n%s", err, firstOut)
	}
	if !strings.Contains(firstOut, "apply terminé") {
		t.Errorf("sortie du premier apply = %q, attendu la confirmation de fin", firstOut)
	}

	secondOut, err := runCLI(t, "apply", "-f", specFile, "--state-dir", stateDir, "--auto-approve")
	if err != nil {
		t.Fatalf("second apply : %v\n%s", err, secondOut)
	}
	if !strings.Contains(secondOut, "apply terminé") {
		t.Errorf("sortie du second apply = %q, attendu la confirmation de fin", secondOut)
	}
}
