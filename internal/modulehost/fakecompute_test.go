// SPDX-License-Identifier: Apache-2.0

package modulehost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	sdk "genesis/sdk/go"
	computevmv1 "genesis/sdk/go/gen/functions/compute/vm/v1"
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

// TestFakeComputeProvidesComputeVM vérifie que fake-compute (docs/07-modules-mvp.md)
// dispense réellement compute.vm/v1 et que EnsureVM est idempotent par nom
// (clé d'idempotence du doc 07).
func TestFakeComputeProvidesComputeVM(t *testing.T) {
	binaryPath, manifest := buildFakeCompute(t)

	client, err := Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch : %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	desc, err := client.Describe(ctx)
	if err != nil {
		t.Fatalf("Describe : %v", err)
	}
	if desc.GetName() != "fake-compute" {
		t.Fatalf("Describe().Name = %q, attendu fake-compute", desc.GetName())
	}

	conn, err := client.DispenseFunction("compute.vm/v1")
	if err != nil {
		t.Fatalf("DispenseFunction : %v", err)
	}
	vmClient := computevmv1.NewComputeVMClient(conn)

	first, err := vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{Name: "infra01", Env: "lab"})
	if err != nil {
		t.Fatalf("EnsureVM (première fois) : %v", err)
	}
	if first.GetId() == "" || first.GetIp() == "" {
		t.Errorf("EnsureVM a retourné une VM incomplète : %+v", first)
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
}
