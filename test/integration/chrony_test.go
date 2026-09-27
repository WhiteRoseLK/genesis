// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	"filippo.io/age"
	"google.golang.org/grpc"

	"github.com/WhiteRoseLK/genesis/internal/broker"
	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/secrets"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// chronyFakeComputeVMServer simulates compute.vm/v1: the real mechanism of
// core.ansible/v1 and of compute.vm/v1 (through fake-compute) is already
// proven for real elsewhere (internal/broker/ansible_test.go,
// TestFakeComputeProvidesComputeVM) — here we only check chrony's own wiring
// (docs/03-module-contract.md rule 7: a module must be testable on its own,
// with simulated required functions).
type chronyFakeComputeVMServer struct {
	computevmv1.UnimplementedComputeVMServer
	mu    sync.Mutex
	vms   map[string]*computevmv1.VM
	calls []*computevmv1.EnsureVMRequest
}

func newChronyFakeComputeVMServer() *chronyFakeComputeVMServer {
	return &chronyFakeComputeVMServer{vms: map[string]*computevmv1.VM{}}
}

func (f *chronyFakeComputeVMServer) EnsureVM(_ context.Context, req *computevmv1.EnsureVMRequest) (*computevmv1.VM, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if vm, ok := f.vms[req.GetName()]; ok {
		return vm, nil
	}
	vm := &computevmv1.VM{Id: "vm-" + req.GetName(), Name: req.GetName(), Ip: "10.42.0.5", Status: "running", SshPort: 22}
	f.vms[req.GetName()] = vm
	return vm, nil
}

func (f *chronyFakeComputeVMServer) DeleteVM(_ context.Context, req *computevmv1.DeleteVMRequest) (*computevmv1.DeleteVMResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.vms, req.GetName())
	return &computevmv1.DeleteVMResponse{}, nil
}

// chronyFakeAnsibleServer records the playbooks it receives and answers
// according to their content (installation vs verification), to exercise both
// of chrony's branches without depending on Docker.
type chronyFakeAnsibleServer struct {
	ansiblev1.UnimplementedAnsibleServer
	mu    sync.Mutex
	calls []*ansiblev1.RunPlaybookRequest
}

func (f *chronyFakeAnsibleServer) RunPlaybook(_ context.Context, req *ansiblev1.RunPlaybookRequest) (*ansiblev1.RunPlaybookResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()

	if strings.Contains(string(req.GetPlaybookYaml()), "wait for a usable synchronisation") {
		// Plausible output of `chronyc tracking`, offset well below 100 ms.
		return &ansiblev1.RunPlaybookResponse{
			Ok: true,
			Output: `TASK [afficher chronyc tracking] ***
ok: [target] => {
    "tracking.stdout": "Reference ID    : 0A0A0005 (10.10.0.5)\nStratum         : 3\nSystem time     : 0.000021345 seconds fast of NTP time\nLeap status     : Normal"
}`,
		}, nil
	}
	return &ansiblev1.RunPlaybookResponse{Ok: true, Output: "ok"}, nil
}

func (f *chronyFakeAnsibleServer) lastCall() *ansiblev1.RunPlaybookRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

func newTestSecretsStore(t *testing.T) *secrets.FileStore {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generating the test identity: %v", err)
	}
	return secrets.NewFileStore(t.TempDir(), identity)
}

// TestChronyProvisionsConfiguresAndVerifies exercises chrony's whole lifecycle
// (Provision -> Configure -> Verify -> Destroy) with simulated compute.vm/v1
// and core.ansible/v1, but a real core.secrets/v1 (age + file): generating the
// service SSH pair is proven for real (docs/08-milestones.md, M6).
func TestChronyProvisionsConfiguresAndVerifies(t *testing.T) {
	binaryPath, manifest := buildModule(t, "chrony")

	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer client.Close()
	ctx := context.Background()

	vmServer := newChronyFakeComputeVMServer()
	ansibleServer := &chronyFakeAnsibleServer{}
	store := newTestSecretsStore(t)

	sessionID := client.Broker().NextId()
	go client.Broker().AcceptAndServe(sessionID, func(opts []grpc.ServerOption) *grpc.Server {
		s := grpc.NewServer(opts...)
		computevmv1.RegisterComputeVMServer(s, vmServer)
		ansiblev1.RegisterAnsibleServer(s, ansibleServer)
		broker.NativeSecrets(store)("chrony")(s)
		return s
	})
	token := strconv.FormatUint(uint64(sessionID), 10)

	checkResp, err := client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if checkResp.GetStatus() != modulev1.CheckResult_STATUS_COMPLIANT {
		t.Fatalf("Check().Status = %v, want COMPLIANT", checkResp.GetStatus())
	}

	provisionResp, err := client.Module().Provision(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if provisionResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Provision().Status = %v", provisionResp.GetStatus())
	}
	if len(vmServer.calls) != 1 || vmServer.calls[0].GetName() != "chrony01" {
		t.Fatalf("EnsureVM called with %+v, want name=chrony01", vmServer.calls)
	}
	if vmServer.calls[0].GetSshPublicKey() == "" {
		t.Error("EnsureVM: empty ssh_public_key, the SSH pair was not generated/passed on")
	}

	configureResp, err := client.Module().Configure(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: provisionResp.GetState()})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if configureResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Configure().Status = %v", configureResp.GetStatus())
	}
	if call := ansibleServer.lastCall(); call == nil || !strings.Contains(string(call.GetPlaybookYaml()), "install chrony") {
		t.Errorf("Configure did not send install_chrony.yml: %+v", call)
	} else if call.GetTarget().GetHost() != "10.42.0.5" {
		t.Errorf("Configure target.host = %q, want 10.42.0.5", call.GetTarget().GetHost())
	}

	verifyResp, err := client.Module().Verify(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: configureResp.GetState()})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verifyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Verify().Status = %v", verifyResp.GetStatus())
	}
	if call := ansibleServer.lastCall(); call == nil || !strings.Contains(string(call.GetPlaybookYaml()), "wait for a usable synchronisation") {
		t.Errorf("Verify did not send check_offset.yml: %+v", call)
	} else if call.GetVars().AsMap()["ntp_server"] != "10.42.0.5" {
		t.Errorf("Verify vars = %+v, want ntp_server=10.42.0.5", call.GetVars().AsMap())
	}
	// EnsureVM of the disposable verifier, then DeleteVM after Verify.
	if len(vmServer.calls) != 2 || vmServer.calls[1].GetName() != "chrony01-verify" {
		t.Fatalf("EnsureVM (verifier) called with %+v, want name=chrony01-verify", vmServer.calls)
	}
	if _, stillThere := vmServer.vms["chrony01-verify"]; stillThere {
		t.Error("the verification VM was not deleted after Verify")
	}

	destroyResp, err := client.Module().Destroy(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: configureResp.GetState()})
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if destroyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Destroy().Status = %v", destroyResp.GetStatus())
	}
	if _, stillThere := vmServer.vms["chrony01"]; stillThere {
		t.Error("the chrony01 VM was not deleted by Destroy")
	}
}

// TestChronyRejectsHighOffset checks that Verify really fails when the offset
// exceeds the threshold (docs/07-mvp-modules.md: "offset < 100 ms") — not a
// silent success.
func TestChronyRejectsHighOffset(t *testing.T) {
	binaryPath, manifest := buildModule(t, "chrony")

	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer client.Close()
	ctx := context.Background()

	vmServer := newChronyFakeComputeVMServer()
	badAnsible := &fakeAnsibleServerWithOffset{offsetSeconds: "0.532000000"}
	store := newTestSecretsStore(t)

	sessionID := client.Broker().NextId()
	go client.Broker().AcceptAndServe(sessionID, func(opts []grpc.ServerOption) *grpc.Server {
		s := grpc.NewServer(opts...)
		computevmv1.RegisterComputeVMServer(s, vmServer)
		ansiblev1.RegisterAnsibleServer(s, badAnsible)
		broker.NativeSecrets(store)("chrony")(s)
		return s
	})
	token := strconv.FormatUint(uint64(sessionID), 10)

	if _, err := client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token}); err != nil {
		t.Fatalf("Check: %v", err)
	}
	provisionResp, err := client.Module().Provision(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	configureResp, err := client.Module().Configure(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: provisionResp.GetState()})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}

	_, err = client.Module().Verify(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: configureResp.GetState()})
	if err == nil {
		t.Fatal("Verify succeeded with a 532 ms offset, want a failure")
	}
}

type fakeAnsibleServerWithOffset struct {
	ansiblev1.UnimplementedAnsibleServer
	offsetSeconds string
}

func (f *fakeAnsibleServerWithOffset) RunPlaybook(_ context.Context, req *ansiblev1.RunPlaybookRequest) (*ansiblev1.RunPlaybookResponse, error) {
	if strings.Contains(string(req.GetPlaybookYaml()), "wait for a usable synchronisation") {
		return &ansiblev1.RunPlaybookResponse{
			Ok:     true,
			Output: `"tracking.stdout": "System time     : ` + f.offsetSeconds + ` seconds slow of NTP time"`,
		}, nil
	}
	return &ansiblev1.RunPlaybookResponse{Ok: true, Output: "ok"}, nil
}
