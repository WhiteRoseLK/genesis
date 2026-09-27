// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
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
