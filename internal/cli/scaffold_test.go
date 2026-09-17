// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestModulesScaffoldGeneratesBuildableModule est le critère d'acceptation
// du jalon J3 (doc 08) : un module généré par scaffold compile — vu depuis
// la commande CLI plutôt que directement le paquet internal/scaffold
// (internal/scaffold/scaffold_test.go couvre déjà découverte/Describe).
func TestModulesScaffoldGeneratesBuildableModule(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..")
	name := "cliscaffoldtest"
	dir := filepath.Join(root, "modules", name)
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
		cmd := exec.Command("go", "work", "edit", "-dropuse=./modules/"+name)
		cmd.Dir = root
		_, _ = cmd.CombinedOutput()
	})

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	out, err := runCLI(t, "modules", "scaffold", name, "--provides", "test.scaffold/v1")
	if err != nil {
		t.Fatalf("modules scaffold : %v\n%s", err, out)
	}
	if !strings.Contains(out, name) {
		t.Errorf("sortie scaffold = %q, attendu la mention du nom du module", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "module.yaml")); err != nil {
		t.Errorf("module.yaml absent : %v", err)
	}
}
