// SPDX-License-Identifier: Apache-2.0

package modulehost

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"genesis/internal/broker"
	"genesis/internal/runner"
	sdk "genesis/sdk/go"
	computevmv1 "genesis/sdk/go/gen/functions/compute/vm/v1"
	modulev1 "genesis/sdk/go/gen/module/v1"
)

func buildFakeCompute(t *testing.T) (binaryPath string, manifest *sdk.ManifestFile) {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(wd, "..", "..", "modules", "fake-compute")

	binaryPath = filepath.Join(t.TempDir(), BinaryName())
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = sourceDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compilation de fake-compute : %v\n%s", err, out)
	}

	manifest, err = sdk.LoadManifest(filepath.Join(sourceDir, "module.yaml"))
	if err != nil {
		t.Fatalf("chargement du manifest : %v", err)
	}
	return binaryPath, manifest
}

// launchFakeComputeWithContainerSession lance fake-compute et lui ouvre une
// session de broker vers core.container/v1 natif — exactement ce que ferait
// internal/engine, reproduit ici pour tester le module isolément.
func launchFakeComputeWithContainerSession(t *testing.T) (*Client, computevmv1.ComputeVMClient) {
	t.Helper()
	ctx := context.Background()

	rt, err := runner.DetectContainerRuntime("auto")
	if err != nil {
		t.Skipf("aucun runtime de conteneur disponible : %v", err)
	}
	registry := broker.NewRegistry()
	registry.SetNative("core.container/v1", broker.NativeContainer(rt))

	binaryPath, manifest := buildFakeCompute(t)
	client, err := Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch : %v", err)
	}
	t.Cleanup(client.Close)

	token := registry.OpenSession(client.Broker(), "fake-compute", []string{"core.container/v1"})
	checkResp, err := client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Check : %v", err)
	}
	if checkResp.GetStatus() != modulev1.CheckResult_STATUS_CONFORME {
		t.Fatalf("Check().Status = %v, attendu CONFORME", checkResp.GetStatus())
	}

	conn, err := client.DispenseFunction("compute.vm/v1")
	if err != nil {
		t.Fatalf("DispenseFunction : %v", err)
	}
	return client, computevmv1.NewComputeVMClient(conn)
}

// generateAuthorizedKey génère une paire de clés ed25519 et renvoie la clé
// publique au format authorized_keys (seule la publique sert ici : c'est ce
// que fake-compute injecte dans le conteneur cible).
func generateAuthorizedKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return string(gossh.MarshalAuthorizedKey(sshPub))
}

// TestFakeComputeProvidesComputeVM vérifie que fake-compute (docs/07-modules-mvp.md)
// crée une vraie « VM » (conteneur SSH-joignable) via core.container/v1, que
// EnsureVM est idempotent par nom, et que DeleteVM l'arrête réellement.
func TestFakeComputeProvidesComputeVM(t *testing.T) {
	client, vmClient := launchFakeComputeWithContainerSession(t)
	ctx := context.Background()

	desc, err := client.Describe(ctx)
	if err != nil {
		t.Fatalf("Describe : %v", err)
	}
	if desc.GetName() != "fake-compute" {
		t.Fatalf("Describe().Name = %q, attendu fake-compute", desc.GetName())
	}

	first, err := vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name: "infra01", Env: "lab", User: "genesis", SshPublicKey: generateAuthorizedKey(t),
	})
	if err != nil {
		t.Fatalf("EnsureVM (première fois) : %v", err)
	}
	if first.GetId() == "" || first.GetIp() == "" {
		t.Fatalf("EnsureVM a retourné une VM incomplète : %+v", first)
	}
	if first.GetSshPort() != 22 {
		t.Errorf("SshPort = %d, attendu 22", first.GetSshPort())
	}
	t.Cleanup(func() {
		_, _ = vmClient.DeleteVM(context.Background(), &computevmv1.DeleteVMRequest{Name: "infra01"})
	})

	// Vérifie que le conteneur tourne réellement, avec l'utilisateur de
	// service demandé (docker exec, pas la parole du module) — même limite
	// d'environnement que internal/broker/ansible_test.go : pas de dial
	// réseau direct ici. -u doit précéder l'ID du conteneur dans docker
	// exec, donc appel direct plutôt que via le helper dockerExec générique.
	// Retenté : le script d'initialisation du conteneur (création de
	// l'utilisateur) prend quelques secondes après que `docker run -d` ait
	// déjà rendu la main.
	var whoamiOut []byte
	for attempt := 0; attempt < 10; attempt++ {
		whoamiCmd := exec.Command("docker", "exec", "-u", "genesis", first.GetId(), "whoami")
		out, err := whoamiCmd.CombinedOutput()
		whoamiOut = out
		if err == nil && string(out) == "genesis\n" {
			break
		}
		time.Sleep(time.Second)
	}
	if string(whoamiOut) != "genesis\n" {
		t.Errorf("whoami (-u genesis) dans le conteneur = %q, attendu genesis", whoamiOut)
	}

	second, err := vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{Name: "infra01", Env: "lab"})
	if err != nil {
		t.Fatalf("EnsureVM (seconde fois) : %v", err)
	}
	if second.GetId() != first.GetId() || second.GetIp() != first.GetIp() {
		t.Errorf("EnsureVM n'est pas idempotent : %+v puis %+v", first, second)
	}

	got, err := vmClient.GetVM(ctx, &computevmv1.GetVMRequest{Name: "infra01"})
	if err != nil {
		t.Fatalf("GetVM : %v", err)
	}
	if got.GetId() != first.GetId() {
		t.Errorf("GetVM = %+v, attendu %+v", got, first)
	}

	now, err := vmClient.Now(ctx, &computevmv1.NowRequest{})
	if err != nil {
		t.Fatalf("Now : %v", err)
	}
	if now.GetTime() == nil {
		t.Error("Now() n'a pas retourné d'horodatage")
	}

	if _, err := vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: "infra01"}); err != nil {
		t.Fatalf("DeleteVM : %v", err)
	}
	if _, err := vmClient.GetVM(ctx, &computevmv1.GetVMRequest{Name: "infra01"}); err == nil {
		t.Error("GetVM après DeleteVM : succès inattendu")
	}
	// DeleteVM est idempotent.
	if _, err := vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: "infra01"}); err != nil {
		t.Errorf("second DeleteVM (déjà supprimée) : erreur inattendue : %v", err)
	}
}
