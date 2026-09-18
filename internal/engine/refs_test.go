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
		t.Fatalf("resolveRefs : %v", err)
	}
	if got["token_id"] != "s3cr3t" {
		t.Errorf("token_id = %v, attendu s3cr3t", got["token_id"])
	}
	if _, ok := got["token_id_ref"]; ok {
		t.Error("la clé _ref originale n'aurait pas dû survivre")
	}
}

func TestResolveRefsMissingEnvErrors(t *testing.T) {
	_, err := resolveRefs(map[string]any{"x_ref": "env://GENESIS_DOES_NOT_EXIST"})
	if err == nil {
		t.Fatal("variable d'environnement absente : succès inattendu")
	}
}

func TestResolveRefsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(path, []byte("valeur-fichier\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveRefs(map[string]any{"x_ref": "file://" + path})
	if err != nil {
		t.Fatalf("resolveRefs : %v", err)
	}
	if got["x"] != "valeur-fichier" {
		t.Errorf("x = %v, attendu valeur-fichier (sans le saut de ligne)", got["x"])
	}
}

func TestResolveRefsVaultNotYetSupported(t *testing.T) {
	_, err := resolveRefs(map[string]any{"x_ref": "vault://secret/foo"})
	if err == nil {
		t.Fatal("référence vault:// : succès inattendu, pas encore prise en charge (J7)")
	}
}

func TestResolveRefsNested(t *testing.T) {
	t.Setenv("GENESIS_TEST_NESTED", "nested-value")
	got, err := resolveRefs(map[string]any{
		"credentials": map[string]any{"token_id_ref": "env://GENESIS_TEST_NESTED"},
	})
	if err != nil {
		t.Fatalf("resolveRefs : %v", err)
	}
	creds, ok := got["credentials"].(map[string]any)
	if !ok {
		t.Fatalf("credentials = %v, attendu une map", got["credentials"])
	}
	if creds["token_id"] != "nested-value" {
		t.Errorf("credentials.token_id = %v, attendu nested-value", creds["token_id"])
	}
}

func TestResolveRefsLeavesOrdinaryFieldsUntouched(t *testing.T) {
	got, err := resolveRefs(map[string]any{"node": "pve01", "count": 3})
	if err != nil {
		t.Fatal(err)
	}
	if got["node"] != "pve01" || got["count"] != 3 {
		t.Errorf("got = %+v, attendu les champs ordinaires inchangés", got)
	}
}
