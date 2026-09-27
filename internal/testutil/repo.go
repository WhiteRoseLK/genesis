// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RepoRoot walks up from the test's directory to the repository root (the one
// that contains go.work).
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
			t.Fatal("repository root (go.work) not found")
		}
		dir = parent
	}
}

// SandboxRepo creates a minimal repository in a temporary directory: a link to
// the real sdk/ and a go.work that only uses it. The tests that generate a
// module (genesis modules scaffold) work there: they touch neither modules/
// nor the real repository's go.work, and can therefore run at the same time as
// the tests of other packages.
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

// goWorkVersion reads the `go` directive of the repository's go.work.
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
	t.Fatalf("no go directive in %s", path)
	return ""
}
