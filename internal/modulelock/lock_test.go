// SPDX-License-Identifier: Apache-2.0

package modulelock

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsEmptyLock(t *testing.T) {
	l, err := Load(filepath.Join(t.TempDir(), "genesis.lock"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(l.Modules) != 0 {
		t.Errorf("Load() on a missing file = %+v, want empty", l.Modules)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "genesis.lock")
	l := &Lock{Modules: map[string]Entry{
		"vault": {Version: "0.1.0", SHA256: "abc123"},
	}}
	if err := l.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Modules["vault"] != l.Modules["vault"] {
		t.Errorf("Load() = %+v, want %+v", got.Modules, l.Modules)
	}
}

// TestVerifyRejectsFingerprintMismatch is the M3 acceptance criterion (doc
// 08): a module whose digest differs from the lock is refused.
func TestVerifyRejectsFingerprintMismatch(t *testing.T) {
	l := &Lock{Modules: map[string]Entry{
		"vault": {Version: "0.1.0", SHA256: "expected-hash"},
	}}

	if err := l.Verify("vault", "0.1.0", "expected-hash"); err != nil {
		t.Errorf("Verify with the expected digest: want success, got %v", err)
	}
	if err := l.Verify("vault", "0.1.0", "tampered-hash"); err == nil {
		t.Error("Verify with a mismatching digest: unexpected success, should have been refused")
	}
	if err := l.Verify("unknown-module", "0.1.0", "whatever"); err == nil {
		t.Error("Verify on a module missing from the lock: unexpected success")
	}
	if err := l.Verify("vault", "9.9.9", "expected-hash"); err == nil {
		t.Error("Verify with a mismatching version: unexpected success")
	}
}
