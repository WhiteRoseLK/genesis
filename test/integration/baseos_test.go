// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	osbasev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

func buildModule(t *testing.T, name string) (binaryPath string, manifest *sdk.ManifestFile) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(wd, "..", "..", "modules", name)

	binaryPath = filepath.Join(t.TempDir(), modulehost.BinaryName())
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = sourceDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compilation de %s: %v\n%s", name, err, out)
	}
	manifest, err = sdk.LoadManifest(filepath.Join(sourceDir, "module.yaml"))
	if err != nil {
		t.Fatalf("loading the manifest of %s: %v", name, err)
	}
	return binaryPath, manifest
}

// fakeAnsibleServer records the RunPlaybook calls it receives, to check what
// base-os really sends without depending on Docker for this test (the
// core.ansible/v1 transport mechanism is already proven for real in
// internal/broker/ansible_test.go).
type fakeAnsibleServer struct {
	ansiblev1.UnimplementedAnsibleServer
	mu    sync.Mutex
	calls []*ansiblev1.RunPlaybookRequest
}

func (f *fakeAnsibleServer) RunPlaybook(_ context.Context, req *ansiblev1.RunPlaybookRequest) (*ansiblev1.RunPlaybookResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	return &ansiblev1.RunPlaybookResponse{Ok: true, Output: "ok"}, nil
}

func (f *fakeAnsibleServer) lastCall() *ansiblev1.RunPlaybookRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

// TestBaseOSCallsAnsibleWithExpectedPlaybooks checks that
// TrustCA/SetResolver/SetNTP dial the broker session captured by Check and
// send the right playbook with the right variables (docs/08-milestones.md,
// M5).
func TestBaseOSCallsAnsibleWithExpectedPlaybooks(t *testing.T) {
	binaryPath, manifest := buildModule(t, "base-os")

	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer client.Close()
	ctx := context.Background()

	fake := &fakeAnsibleServer{}
	sessionID := client.Broker().NextId()
	go client.Broker().AcceptAndServe(sessionID, func(opts []grpc.ServerOption) *grpc.Server {
		s := grpc.NewServer(opts...)
		ansiblev1.RegisterAnsibleServer(s, fake)
		return s
	})

	// Simulates what the engine does: Check first, with the session token, so
	// that base-os captures it (docs/02-architecture.md).
	token := strconv.FormatUint(uint64(sessionID), 10)
	checkResp, err := client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if checkResp.GetStatus() != modulev1.CheckResult_STATUS_COMPLIANT {
		t.Fatalf("Check().Status = %v, want COMPLIANT", checkResp.GetStatus())
	}

	conn, err := client.DispenseFunction("os.base/v1")
	if err != nil {
		t.Fatalf("DispenseFunction: %v", err)
	}
	osBase := osbasev1.NewBaseClient(conn)

	target := &osbasev1.Target{Host: "10.0.0.5", Port: 22, User: "genesis", SshPrivateKey: "test-key"}

	if _, err := osBase.TrustCA(ctx, &osbasev1.TrustCARequest{Target: target, CaCertPem: "CERT-PEM"}); err != nil {
		t.Fatalf("TrustCA: %v", err)
	}
	if call := fake.lastCall(); call == nil || !strings.Contains(string(call.GetPlaybookYaml()), "install the CA certificate") {
		t.Errorf("TrustCA did not send the trust_ca.yml playbook: %+v", call)
	} else if call.GetVars().AsMap()["ca_cert_pem"] != "CERT-PEM" {
		t.Errorf("TrustCA vars = %+v, want ca_cert_pem=CERT-PEM", call.GetVars().AsMap())
	}

	if _, err := osBase.SetResolver(ctx, &osbasev1.SetResolverRequest{Target: target, Nameservers: []string{"10.10.0.5"}, Domain: "lab.internal"}); err != nil {
		t.Fatalf("SetResolver: %v", err)
	}
	if call := fake.lastCall(); call == nil || !strings.Contains(string(call.GetPlaybookYaml()), "configure the DNS resolver") {
		t.Errorf("SetResolver did not send the set_resolver.yml playbook: %+v", call)
	}

	if _, err := osBase.SetNTP(ctx, &osbasev1.SetNTPRequest{Target: target, Servers: []string{"10.10.0.6"}}); err != nil {
		t.Fatalf("SetNTP: %v", err)
	}
	if call := fake.lastCall(); call == nil || !strings.Contains(string(call.GetPlaybookYaml()), "configure the NTP servers") {
		t.Errorf("SetNTP did not send the set_ntp.yml playbook: %+v", call)
	}

	// Harden stays an explicit stub for this milestone (see docs/PROGRESS.md).
	if _, err := osBase.Harden(ctx, &osbasev1.HardenRequest{Target: target}); err == nil {
		t.Error("Harden: unexpected success, should still be Unimplemented")
	}
}
