// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// repoRoot locates the module root (this package's directory is
// internal/scaffold, two levels below the repo root).
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}

// TestGenerateProducesBuildableConformingModule est le critère d'acceptation
// du jalon J3 (doc 08) : un module généré par scaffold compile et passe sa
// propre suite de conformité.
func TestGenerateProducesBuildableConformingModule(t *testing.T) {
	root := repoRoot(t)
	name := "scaffoldtest"
	dir := filepath.Join(root, "modules", name)
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
		removeGoWorkUse(t, root, filepath.Join(".", "modules", name))
	})

	got, err := Generate(root, name, []string{"dns.zone/v1", "dns.resolver/v1"})
	if err != nil {
		t.Fatalf("Generate : %v", err)
	}
	if got != dir {
		t.Fatalf("Generate a retourné %q, attendu %q", got, dir)
	}

	for _, f := range []string{"go.mod", "module.yaml", "main.go", "schema.json", "conformance_test.go"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("fichier attendu absent : %s : %v", f, err)
		}
	}

	build := exec.Command("go", "build", "./...")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build sur le module généré : %v\n%s", err, out)
	}

	conformance := exec.Command("go", "test", "./...", "-run", "Conformance", "-v")
	conformance.Dir = dir
	if out, err := conformance.CombinedOutput(); err != nil {
		t.Fatalf("suite de conformité sur le module généré : %v\n%s", err, out)
	}
}

func removeGoWorkUse(t *testing.T, root, useDir string) {
	t.Helper()
	cmd := exec.Command("go", "work", "edit", "-dropuse="+useDir)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("nettoyage go.work (non bloquant) : %v\n%s", err, out)
	}
}
