// SPDX-License-Identifier: Apache-2.0

package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/testutil"
)

// TestGenerateProducesBuildableConformingModule est le critère d'acceptation
// du jalon J3 (doc 08) : un module généré par scaffold compile et passe sa
// propre suite de conformité. Il est généré dans un dépôt temporaire, jamais
// dans le vrai (testutil.SandboxRepo).
func TestGenerateProducesBuildableConformingModule(t *testing.T) {
	root := testutil.SandboxRepo(t)
	name := "scaffoldtest"
	dir := filepath.Join(root, "modules", name)

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
