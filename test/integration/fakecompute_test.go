// SPDX-License-Identifier: Apache-2.0

//go:build docker

package integration

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

	"github.com/WhiteRoseLK/genesis/internal/broker"
	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/testutil"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

func buildFakeCompute(t *testing.T) (binaryPath string, manifest *sdk.ManifestFile) {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(wd, "..", "..", "modules", "fake-compute")

	binaryPath = filepath.Join(t.TempDir(), modulehost.BinaryName())
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = sourceDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compilation de fake-compute: %v\n%s", err, out)
	}

	manifest, err = sdk.LoadManifest(filepath.Join(sourceDir, "module.yaml"))
	if err != nil {
		t.Fatalf("loading the manifest: %v", err)
	}
	return binaryPath, manifest
}

// launchFakeComputeWithContainerSession launches fake-compute and opens a
// broker session to the native core.container/v1 for it — exactly what
// internal/engine would do, reproduced here to test the module in isolation.
func launchFakeComputeWithContainerSession(t *testing.T) (*modulehost.Client, computevmv1.ComputeVMClient) {
	t.Helper()
	ctx := context.Background()

	rt := testutil.RequireRuntime(t)
	registry := broker.NewRegistry()
	registry.SetNative("core.container/v1", broker.NativeContainer(rt))

	binaryPath, manifest := buildFakeCompute(t)
	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(client.Close)

	token := registry.OpenSession(client.Broker(), "fake-compute", []string{"core.container/v1"})
	checkResp, err := client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if checkResp.GetStatus() != modulev1.CheckResult_STATUS_COMPLIANT {
		t.Fatalf("Check().Status = %v, want COMPLIANT", checkResp.GetStatus())
	}

	conn, err := client.DispenseFunction("compute.vm/v1")
	if err != nil {
		t.Fatalf("DispenseFunction: %v", err)
	}
	return client, computevmv1.NewComputeVMClient(conn)
}

// generateAuthorizedKey generates an ed25519 key pair and returns the public
// key in authorized_keys format (only the public key matters here: it is what
// fake-compute injects into the target container).
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

// TestFakeComputeProvidesComputeVM checks that fake-compute
// (docs/07-mvp-modules.md) creates a real "VM" (SSH-reachable container)
// through core.container/v1, that EnsureVM is idempotent by name, and that
// DeleteVM really stops it.
func TestFakeComputeProvidesComputeVM(t *testing.T) {
	client, vmClient := launchFakeComputeWithContainerSession(t)
	ctx := context.Background()

	desc, err := client.Describe(ctx)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if desc.GetName() != "fake-compute" {
		t.Fatalf("Describe().Name = %q, want fake-compute", desc.GetName())
	}

	first, err := vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name: "infra01", Env: "lab", User: "genesis", SshPublicKey: generateAuthorizedKey(t),
	})
	if err != nil {
		t.Fatalf("EnsureVM (first time): %v", err)
	}
	if first.GetId() == "" || first.GetIp() == "" {
		t.Fatalf("EnsureVM returned an incomplete VM: %+v", first)
	}
	if first.GetSshPort() != 22 {
		t.Errorf("SshPort = %d, want 22", first.GetSshPort())
	}
	t.Cleanup(func() {
		_, _ = vmClient.DeleteVM(context.Background(), &computevmv1.DeleteVMRequest{Name: "infra01"})
	})

	// Check that the container is really running, with the requested service
	// user (docker exec, not the module's word) — the same environment limit
	// as internal/broker/ansible_test.go: no direct network dial here. -u must
	// come before the container ID in docker exec, hence a direct call rather
	// than the generic dockerExec helper. Retried: the container's init script
	// (user creation) takes a few seconds after `docker run -d` has already
	// returned.
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
		t.Errorf("whoami (-u genesis) in the container = %q, want genesis", whoamiOut)
	}

	second, err := vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{Name: "infra01", Env: "lab"})
	if err != nil {
		t.Fatalf("EnsureVM (second time): %v", err)
	}
	if second.GetId() != first.GetId() || second.GetIp() != first.GetIp() {
		t.Errorf("EnsureVM is not idempotent: %+v then %+v", first, second)
	}

	got, err := vmClient.GetVM(ctx, &computevmv1.GetVMRequest{Name: "infra01"})
	if err != nil {
		t.Fatalf("GetVM: %v", err)
	}
	if got.GetId() != first.GetId() {
		t.Errorf("GetVM = %+v, want %+v", got, first)
	}

	now, err := vmClient.Now(ctx, &computevmv1.NowRequest{})
	if err != nil {
		t.Fatalf("Now: %v", err)
	}
	if now.GetTime() == nil {
		t.Error("Now() returned no timestamp")
	}

	if _, err := vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: "infra01"}); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if _, err := vmClient.GetVM(ctx, &computevmv1.GetVMRequest{Name: "infra01"}); err == nil {
		t.Error("GetVM after DeleteVM: unexpected success")
	}
	// DeleteVM is idempotent.
	if _, err := vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: "infra01"}); err != nil {
		t.Errorf("second DeleteVM (already deleted): unexpected error: %v", err)
	}
}
