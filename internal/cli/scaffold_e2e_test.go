// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"genesis/internal/modulehost"
)

// TestScaffoldInstallDiscoverDescribe est le critère d'acceptation du
// jalon J3 dans son ensemble (doc 08) : "un module généré par scaffold
// compile, est découvert, répond à Describe" — enchaîné de bout en bout
// (scaffold -> install -> Discover -> Launch -> Describe via gRPC réel),
// plutôt que vérifié par des tests séparés qui ne couvrent chacun qu'un
// maillon.
func TestScaffoldInstallDiscoverDescribe(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..")
	name := "e2escaffoldtest"
	sourceDir := filepath.Join(root, "modules", name)
	t.Cleanup(func() {
		_ = os.RemoveAll(sourceDir)
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

	if _, err := runCLI(t, "modules", "scaffold", name, "--provides", "test.e2e/v1"); err != nil {
		t.Fatalf("modules scaffold : %v", err)
	}

	useTempHomeKeepingGoCache(t)
	lockFile := filepath.Join(t.TempDir(), "genesis.lock")
	if out, err := runCLI(t, "modules", "install", sourceDir, "--lock-file", lockFile); err != nil {
		t.Fatalf("modules install : %v\n%s", err, out)
	}

	// Découverte : le module installé doit apparaître sous les répertoires
	// de recherche standards (docs/02-architecture.md).
	found, err := modulehost.Discover(modulehost.SearchPaths())
	if err != nil {
		t.Fatalf("Discover : %v", err)
	}
	var installed *modulehost.Installed
	for i := range found {
		if found[i].Name == name {
			installed = &found[i]
		}
	}
	if installed == nil {
		t.Fatalf("module %q non découvert parmi %+v", name, found)
	}

	// Describe via le vrai protocole gRPC/go-plugin, pas un appel Go direct.
	client, err := modulehost.Launch(installed.BinaryPath, installed.Manifest)
	if err != nil {
		t.Fatalf("Launch : %v", err)
	}
	defer client.Close()

	manifest, err := client.Describe(context.Background())
	if err != nil {
		t.Fatalf("Describe : %v", err)
	}
	if manifest.GetName() != name {
		t.Errorf("Describe().Name = %q, attendu %q", manifest.GetName(), name)
	}
	if len(manifest.GetProvides()) != 1 || manifest.GetProvides()[0].GetFunction() != "test.e2e/v1" {
		t.Errorf("Describe().Provides = %+v, attendu [test.e2e/v1]", manifest.GetProvides())
	}
}
