// SPDX-License-Identifier: Apache-2.0

package state

import "testing"

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := New()
	want.SecretsBackend = "vault"

	if err := Save(dir, want); err != nil {
		t.Fatalf("Save : %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if got.SchemaVersion != want.SchemaVersion || got.SecretsBackend != want.SecretsBackend {
		t.Errorf("Load() = %+v, attendu %+v", got, want)
	}
}

func TestLoadWithoutInit(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("Load sans état existant : succès inattendu")
	}
}
