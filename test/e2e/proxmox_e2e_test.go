// SPDX-License-Identifier: Apache-2.0

//go:build integration

package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

func buildModule(t *testing.T, name string) (binaryPath string, manifest *sdk.ManifestFile) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	sourceDir := filepath.Join(repoRoot, "modules", name)

	binaryPath = filepath.Join(t.TempDir(), modulehost.BinaryName())
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = sourceDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compilation of %s: %v\n%s", name, err, out)
	}
	manifest, err = sdk.LoadManifest(filepath.Join(sourceDir, "module.yaml"))
	if err != nil {
		t.Fatalf("loading manifest of %s: %v", name, err)
	}
	return binaryPath, manifest
}

func getEnvOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func generateSSHKeyPair(t *testing.T) (pubKeyStr, privKeyPEM string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating ed25519 key: %v", err)
	}
	sshPubKey, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("converting ssh public key: %v", err)
	}
	pubKeyStr = string(ssh.MarshalAuthorizedKey(sshPubKey))

	privBytes, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshaling private key: %v", err)
	}
	privKeyPEM = string(pem.EncodeToMemory(privBytes))
	return pubKeyStr, privKeyPEM
}

func TestProxmoxRealInfrastructureE2E(t *testing.T) {
	endpoint := os.Getenv("PROXMOX_ENDPOINT")
	tokenID := os.Getenv("PROXMOX_TOKEN_ID")
	tokenSecret := os.Getenv("PROXMOX_TOKEN_SECRET")

	if endpoint == "" || tokenID == "" || tokenSecret == "" {
		t.Skip("skipping real Proxmox E2E test: PROXMOX_ENDPOINT, PROXMOX_TOKEN_ID and PROXMOX_TOKEN_SECRET must be set")
	}

	node := getEnvOrDefault("PROXMOX_NODE", "proxmox")
	storage := getEnvOrDefault("PROXMOX_STORAGE", "SSD")
	bridge := getEnvOrDefault("PROXMOX_BRIDGE", "vmbr0")
	image := getEnvOrDefault("PROXMOX_IMAGE", "genesis-template")
	vmidMinStr := getEnvOrDefault("PROXMOX_VMID_MIN", "3000")
	vmidMin, err := strconv.Atoi(vmidMinStr)
	if err != nil {
		t.Fatalf("invalid PROXMOX_VMID_MIN %q: %v", vmidMinStr, err)
	}

	binaryPath, manifest := buildModule(t, "proxmox")
	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch proxmox: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cfgMap := map[string]any{
		"endpoint":     endpoint,
		"node":         node,
		"storage":      storage,
		"bridge":       bridge,
		"image":        image,
		"vmid_min":     vmidMin,
		"full_clone":   true,
		"tls_insecure": true,
		"credentials": map[string]any{
			"token_id":     tokenID,
			"token_secret": tokenSecret,
		},
	}
	cfgStruct, err := structpb.NewStruct(cfgMap)
	if err != nil {
		t.Fatalf("creating config struct: %v", err)
	}

	// 1. Check phase: connects to real Proxmox API
	checkResp, err := client.Module().Check(ctx, &modulev1.StepRequest{
		RunId:  "e2e-proxmox",
		Config: cfgStruct,
	})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if checkResp.GetStatus() != modulev1.CheckResult_STATUS_COMPLIANT {
		t.Fatalf("Check status = %v, want COMPLIANT", checkResp.GetStatus())
	}

	// 2. Dispense compute.vm/v1
	conn, err := client.DispenseFunction("compute.vm/v1")
	if err != nil {
		t.Fatalf("DispenseFunction compute.vm/v1: %v", err)
	}
	vmClient := computevmv1.NewComputeVMClient(conn)

	// 3. EnsureImage
	if _, err := vmClient.EnsureImage(ctx, &computevmv1.EnsureImageRequest{Image: image}); err != nil {
		t.Fatalf("EnsureImage(%q): %v", image, err)
	}

	// 4. EnsureVM: provision test VM in 3xxx range
	sshPubKey, _ := generateSSHKeyPair(t)
	vmName := "genesis-e2e-test"

	// Cleanup any previous leftover VM with this name
	_, _ = vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: vmName})

	vm1, err := vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name:         vmName,
		Env:          "lab",
		Ip:           "192.168.1.240/24",
		Gateway:      "192.168.1.254",
		User:         "genesis",
		SshPublicKey: sshPubKey,
	})
	if err != nil {
		t.Fatalf("EnsureVM (first call): %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		_, _ = vmClient.DeleteVM(cleanupCtx, &computevmv1.DeleteVMRequest{Name: vmName})
	})

	vmID, err := strconv.Atoi(vm1.GetId())
	if err != nil {
		t.Fatalf("invalid VM ID %q: %v", vm1.GetId(), err)
	}
	if vmID < vmidMin {
		t.Errorf("VM ID %d is below requested minimum %d (must be in 3xxx range)", vmID, vmidMin)
	}
	if vm1.GetStatus() != "running" {
		t.Errorf("VM status = %q, want running", vm1.GetStatus())
	}
	if vm1.GetSshPort() != 22 {
		t.Errorf("VM ssh_port = %d, want 22", vm1.GetSshPort())
	}

	// 5. Idempotence: second EnsureVM must return the identical VM without re-creating
	vm2, err := vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name: vmName,
		Env:  "lab",
	})
	if err != nil {
		t.Fatalf("EnsureVM (idempotence call): %v", err)
	}
	if vm2.GetId() != vm1.GetId() {
		t.Errorf("EnsureVM not idempotent: id1=%s, id2=%s", vm1.GetId(), vm2.GetId())
	}

	// 6. GetVM
	got, err := vmClient.GetVM(ctx, &computevmv1.GetVMRequest{Name: vmName})
	if err != nil {
		t.Fatalf("GetVM: %v", err)
	}
	if got.GetId() != vm1.GetId() {
		t.Errorf("GetVM id = %s, want %s", got.GetId(), vm1.GetId())
	}
	if got.GetStatus() != "running" {
		t.Errorf("GetVM status = %q, want running", got.GetStatus())
	}

	// 7. Now
	nowResp, err := vmClient.Now(ctx, &computevmv1.NowRequest{})
	if err != nil {
		t.Fatalf("Now: %v", err)
	}
	nowTime := nowResp.GetTime().AsTime()
	if time.Since(nowTime).Abs() > 5*time.Minute {
		t.Errorf("Now returned %v, too far from current time %v", nowTime, time.Now())
	}

	// 8. DeleteVM
	if _, err := vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: vmName}); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}

	// Verify VM is deleted
	if _, err := vmClient.GetVM(ctx, &computevmv1.GetVMRequest{Name: vmName}); err == nil {
		t.Error("GetVM after DeleteVM succeeded, expected not found error")
	}

	// Second DeleteVM is idempotent
	if _, err := vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: vmName}); err != nil {
		t.Errorf("second DeleteVM returned unexpected error: %v", err)
	}
}
