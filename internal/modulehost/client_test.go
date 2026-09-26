// SPDX-License-Identifier: Apache-2.0

package modulehost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// buildPanickingModule compile test/modules/panicking (docs/02-architecture.md :
// module de test) vers un binaire temporaire nommé comme l'attend le layout
// d'installation, et le retourne.
func buildPanickingModule(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(wd, "..", "..", "test", "modules", "panicking")

	binaryPath := filepath.Join(t.TempDir(), BinaryName())
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = sourceDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compilation du module de test panicking : %v\n%s", err, out)
	}
	return binaryPath
}

// TestModuleCrashProducesCleanError est le critère d'acceptation du jalon J3
// (doc 08) : un module qui plante pendant une étape produit une erreur
// propre sans arrêter le cœur — le process de test doit rester vivant et
// obtenir une erreur exploitable, pas un panic qui remonte jusqu'ici.
func TestModuleCrashProducesCleanError(t *testing.T) {
	binaryPath := buildPanickingModule(t)

	client, err := Launch(binaryPath, nil)
	if err != nil {
		t.Fatalf("Launch : %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Le module répond normalement à Describe avant de planter sur Check.
	manifest, err := client.Describe(ctx)
	if err != nil {
		t.Fatalf("Describe : %v", err)
	}
	if manifest.GetName() != "panicking" {
		t.Fatalf("Describe().Name = %q, attendu %q", manifest.GetName(), "panicking")
	}

	_, err = client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test"})
	if err == nil {
		t.Fatal("Check sur un module qui panique : succès inattendu")
	}
	t.Logf("erreur propre obtenue comme attendu : %v", err)

	// Le cœur (ce process de test) est toujours vivant ici : c'est le point
	// du test. Une seconde requête sur la même connexion doit échouer
	// proprement aussi, pas provoquer un nouveau crash caché.
	if _, err := client.Describe(ctx); err == nil {
		t.Log("Describe après crash a réussi (le plugin a pu redémarrer) — acceptable, le cœur n'a pas planté")
	}
}
