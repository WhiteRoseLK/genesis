// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSuccess(t *testing.T) {
	useTempHomeKeepingGoCache(t)
	lockFile := filepath.Join(t.TempDir(), "genesis.lock")
	installChainModules(t, lockFile)

	specFile := writeChainSpec(t)
	out, err := runCLI(t, "validate", "-f", specFile)
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}

	if !strings.Contains(out, "valid structure and module resolution") {
		t.Errorf("validate stdout = %q, want valid structure message", out)
	}
	if !strings.Contains(out, "all modules validated successfully") {
		t.Errorf("validate stdout = %q, want all modules validated successfully", out)
	}
}

func TestValidateInvalidSpec(t *testing.T) {
	invalidSpec := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(invalidSpec, []byte("invalid: yaml: content: ["), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runCLI(t, "validate", "-f", invalidSpec)
	if err == nil {
		t.Fatalf("validate should have failed for invalid yaml, got success:\n%s", out)
	}
}

func TestValidateMissingModule(t *testing.T) {
	useTempHomeKeepingGoCache(t)
	specWithMissing := filepath.Join(t.TempDir(), "missing.yaml")
	content := `apiVersion: genesis/v1alpha1
kind: Environment
metadata: { name: lab }
network: { cidr: 10.10.0.0/24, gateway: 10.10.0.1, pool: 10.10.0.50-10.10.0.99, domain: lab.internal }
seed: { address: 10.10.0.10 }
capabilities:
  unknown: { module: nonexistent-module }
`
	if err := os.WriteFile(specWithMissing, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runCLI(t, "validate", "-f", specWithMissing)
	if err == nil {
		t.Fatalf("validate should have failed for missing module, got success:\n%s", out)
	}
}
