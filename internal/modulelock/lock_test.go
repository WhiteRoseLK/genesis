// SPDX-License-Identifier: Apache-2.0

package modulelock

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsEmptyLock(t *testing.T) {
	l, err := Load(filepath.Join(t.TempDir(), "genesis.lock"))
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if len(l.Modules) != 0 {
		t.Errorf("Load() sur un fichier absent = %+v, attendu vide", l.Modules)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "genesis.lock")
	l := &Lock{Modules: map[string]Entry{
		"vault": {Version: "0.1.0", SHA256: "abc123"},
	}}
	if err := l.Save(path); err != nil {
		t.Fatalf("Save : %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if got.Modules["vault"] != l.Modules["vault"] {
		t.Errorf("Load() = %+v, attendu %+v", got.Modules, l.Modules)
	}
}

// TestVerifyRejectsFingerprintMismatch est le critère d'acceptation du
// jalon J3 (doc 08) : un module dont l'empreinte diffère du lock est refusé.
func TestVerifyRejectsFingerprintMismatch(t *testing.T) {
	l := &Lock{Modules: map[string]Entry{
		"vault": {Version: "0.1.0", SHA256: "expected-hash"},
	}}

	if err := l.Verify("vault", "0.1.0", "expected-hash"); err != nil {
		t.Errorf("Verify avec l'empreinte attendue : succès attendu, obtenu %v", err)
	}
	if err := l.Verify("vault", "0.1.0", "tampered-hash"); err == nil {
		t.Error("Verify avec une empreinte divergente : succès inattendu, devait être refusé")
	}
	if err := l.Verify("unknown-module", "0.1.0", "whatever"); err == nil {
		t.Error("Verify sur un module absent du lock : succès inattendu")
	}
	if err := l.Verify("vault", "9.9.9", "expected-hash"); err == nil {
		t.Error("Verify avec une version divergente : succès inattendu")
	}
}
