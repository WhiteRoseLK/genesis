// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRefsEnv(t *testing.T) {
	t.Setenv("GENESIS_TEST_TOKEN", "s3cr3t")
	got, err := resolveRefs(map[string]any{"token_id_ref": "env://GENESIS_TEST_TOKEN"})
	if err != nil {
		t.Fatalf("resolveRefs: %v", err)
	}
	if got["token_id"] != "s3cr3t" {
		t.Errorf("token_id = %v, want s3cr3t", got["token_id"])
	}
	if _, ok := got["token_id_ref"]; ok {
		t.Error("the original _ref key should not have survived")
	}
}

func TestResolveRefsMissingEnvErrors(t *testing.T) {
	_, err := resolveRefs(map[string]any{"x_ref": "env://GENESIS_DOES_NOT_EXIST"})
	if err == nil {
		t.Fatal("environment variable not set: unexpected success")
	}
}

func TestResolveRefsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte("file-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveRefs(map[string]any{"x_ref": "file://" + path})
	if err != nil {
		t.Fatalf("resolveRefs: %v", err)
	}
	if got["x"] != "file-value" {
		t.Errorf("x = %v, want file-value (without the newline)", got["x"])
	}
}

func TestResolveRefsVaultNotYetSupported(t *testing.T) {
	_, err := resolveRefs(map[string]any{"x_ref": "vault://secret/foo"})
	if err == nil {
		t.Fatal("vault:// reference: unexpected success, not supported yet")
	}
}

func TestResolveRefsNested(t *testing.T) {
	t.Setenv("GENESIS_TEST_NESTED", "nested-value")
	got, err := resolveRefs(map[string]any{
		"credentials": map[string]any{"token_id_ref": "env://GENESIS_TEST_NESTED"},
	})
	if err != nil {
		t.Fatalf("resolveRefs: %v", err)
	}
	creds, ok := got["credentials"].(map[string]any)
	if !ok {
		t.Fatalf("credentials = %v, want a map", got["credentials"])
	}
	if creds["token_id"] != "nested-value" {
		t.Errorf("credentials.token_id = %v, want nested-value", creds["token_id"])
	}
}

func TestResolveRefsLeavesOrdinaryFieldsUntouched(t *testing.T) {
	got, err := resolveRefs(map[string]any{"node": "pve01", "count": 3})
	if err != nil {
		t.Fatal(err)
	}
	if got["node"] != "pve01" || got["count"] != 3 {
		t.Errorf("got = %+v, want the ordinary fields unchanged", got)
	}
}
