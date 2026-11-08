// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/secrets"
)

func TestSecretsListAndGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")

	if _, err := runCLI(t, "init", "--state-dir", stateDir); err != nil {
		t.Fatalf("init: %v", err)
	}

	provider := secrets.FileMasterKeyProvider{StateDir: stateDir}
	identity, err := provider.Load(context.Background())
	if err != nil {
		t.Fatalf("loading the master key: %v", err)
	}
	store := secrets.NewFileStore(stateDir, identity)
	if err := store.Put(context.Background(), secrets.Ref("pki/root-ca-key"), secrets.NewSecret("secret-value"), secrets.Meta{Kind: "private-key", Owner: "pki"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	listOut, err := runCLI(t, "secrets", "list", "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("secrets list: %v", err)
	}
	if !strings.Contains(listOut, "pki/root-ca-key") || !strings.Contains(listOut, "kind=private-key") {
		t.Errorf("secrets list = %q, want the reference and its kind", listOut)
	}
	if strings.Contains(listOut, "secret-value") {
		t.Errorf("secrets list exposed the secret value: %q", listOut)
	}

	getOut, err := runCLI(t, "secrets", "get", "pki/root-ca-key", "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("secrets get: %v", err)
	}
	if strings.TrimSpace(getOut) != "secret-value" {
		t.Errorf("secrets get = %q, want %q", strings.TrimSpace(getOut), "secret-value")
	}
}

func TestSecretsCommandsRequireInit(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	if _, err := runCLI(t, "secrets", "list", "--state-dir", stateDir); err == nil {
		t.Fatal("secrets list without a prior init: unexpected success")
	}
}

// Regression for #132: `secrets` takes the state directory from the spec with
// -f, like status, init and clean, unless --state-dir is explicit.
func TestSecretsReadStateDirFromSpec(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	specFile := filepath.Join(t.TempDir(), "spec.yaml")
	content := `apiVersion: genesis/v1alpha1
kind: Environment
metadata: { name: lab }
network: { cidr: 10.10.0.0/24, gateway: 10.10.0.1, pool: 10.10.0.50-10.10.0.99, domain: lab.internal }
seed: { address: 10.10.0.10, state_dir: ` + stateDir + ` }
capabilities:
  test-a: { module: test-a }
`
	if err := os.WriteFile(specFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runCLI(t, "init", "-f", specFile); err != nil {
		t.Fatalf("init -f: %v", err)
	}
	provider := secrets.FileMasterKeyProvider{StateDir: stateDir}
	identity, err := provider.Load(context.Background())
	if err != nil {
		t.Fatalf("loading the master key: %v", err)
	}
	if err := secrets.NewFileStore(stateDir, identity).Put(context.Background(), secrets.Ref("pki/root-ca"), secrets.NewSecret("from-spec"), secrets.Meta{Kind: "certificate", Owner: "pki"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	out, err := runCLI(t, "secrets", "get", "-f", specFile, "pki/root-ca")
	if err != nil {
		t.Fatalf("secrets get -f: %v", err)
	}
	if strings.TrimSpace(out) != "from-spec" {
		t.Errorf("secrets get -f = %q, want %q", strings.TrimSpace(out), "from-spec")
	}
	if out, err := runCLI(t, "secrets", "list", "-f", specFile); err != nil || !strings.Contains(out, "pki/root-ca") {
		t.Errorf("secrets list -f = %q, %v; want the reference", out, err)
	}
}

func TestSecretsRejectsUnreadableSpec(t *testing.T) {
	if _, err := runCLI(t, "secrets", "list", "-f", filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("secrets list -f with a missing spec: unexpected success")
	}
}
