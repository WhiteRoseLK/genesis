// SPDX-License-Identifier: Apache-2.0

package modulehost

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintMatchesSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "module-linux-amd64")
	content := []byte("fake binary content")
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Fingerprint(path)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Errorf("Fingerprint = %q, want %q", got, want)
	}
}

func TestFingerprintChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a")
	pathB := filepath.Join(dir, "b")
	if err := os.WriteFile(pathA, []byte("content A"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, []byte("content B"), 0o755); err != nil {
		t.Fatal(err)
	}

	fpA, err := Fingerprint(pathA)
	if err != nil {
		t.Fatal(err)
	}
	fpB, err := Fingerprint(pathB)
	if err != nil {
		t.Fatal(err)
	}
	if fpA == fpB {
		t.Error("two different binaries have the same digest")
	}
}
