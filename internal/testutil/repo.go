// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RepoRoot remonte depuis le répertoire du test jusqu'à la racine du dépôt
// (celle qui contient go.work).
func RepoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("racine du dépôt (go.work) introuvable")
		}
		dir = parent
	}
}

// SandboxRepo crée un dépôt minimal dans un répertoire temporaire : un lien
// vers le vrai sdk/ et un go.work qui n'utilise que lui. Les tests qui
// génèrent un module (genesis modules scaffold) y travaillent : ils ne
// touchent ni modules/ ni le go.work du vrai dépôt, et peuvent donc tourner
// en même temps que les tests des autres paquets.
func SandboxRepo(t testing.TB) string {
	t.Helper()
	real := RepoRoot(t)
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(real, "sdk"), filepath.Join(root, "sdk")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "modules"), 0o750); err != nil {
		t.Fatal(err)
	}
	goWork := "go " + goWorkVersion(t, filepath.Join(real, "go.work")) + "\n\nuse ./sdk\n"
	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte(goWork), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// goWorkVersion lit la directive `go` du go.work du dépôt.
func goWorkVersion(t testing.TB, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "go "); ok {
			return v
		}
	}
	t.Fatalf("directive go absente de %s", path)
	return ""
}
