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

// chronyFakeComputeVMServer simule compute.vm/v1 : le mécanisme réel de
// core.ansible/v1 et de compute.vm/v1 (via fake-compute) est déjà prouvé
// pour de vrai ailleurs (internal/broker/ansible_test.go,
// TestFakeComputeProvidesComputeVM) — ici on vérifie seulement le
// câblage propre à chrony (docs/03-module-contract.md règle 7 : un module
// doit être testable seul, fonctions requises simulées).
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

// chronyFakeAnsibleServer capture les playbooks reçus et répond en fonction du
// contenu (installation vs vérification), pour exercer les deux branches
// de chrony sans dépendre de Docker.
type chronyFakeAnsibleServer struct {
	ansiblev1.UnimplementedAnsibleServer
	mu    sync.Mutex
	calls []*ansiblev1.RunPlaybookRequest
}

func (f *chronyFakeAnsibleServer) RunPlaybook(_ context.Context, req *ansiblev1.RunPlaybookRequest) (*ansiblev1.RunPlaybookResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()

	if strings.Contains(string(req.GetPlaybookYaml()), "attendre une synchronisation exploitable") {
		// Sortie plausible de `chronyc tracking`, écart bien sous 100 ms.
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
		t.Fatalf("génération de l'identité de test : %v", err)
	}
	return secrets.NewFileStore(t.TempDir(), identity)
}

// TestChronyProvisionsConfiguresAndVerifies exerce tout le cycle de vie de
// chrony (Provision -> Configure -> Verify -> Destroy) avec compute.vm/v1
// et core.ansible/v1 simulés, mais core.secrets/v1 réel (age + fichier) :
// la génération de la paire SSH de service est prouvée pour de vrai
// (docs/08-milestones.md, J6).
func TestChronyProvisionsConfiguresAndVerifies(t *testing.T) {
	binaryPath, manifest := buildModule(t, "chrony")

	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch : %v", err)
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
		t.Fatalf("Check : %v", err)
	}
	if checkResp.GetStatus() != modulev1.CheckResult_STATUS_COMPLIANT {
		t.Fatalf("Check().Status = %v, attendu CONFORME", checkResp.GetStatus())
	}

	provisionResp, err := client.Module().Provision(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Provision : %v", err)
	}
	if provisionResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Provision().Status = %v", provisionResp.GetStatus())
	}
	if len(vmServer.calls) != 1 || vmServer.calls[0].GetName() != "chrony01" {
		t.Fatalf("EnsureVM appelé avec %+v, attendu name=chrony01", vmServer.calls)
	}
	if vmServer.calls[0].GetSshPublicKey() == "" {
		t.Error("EnsureVM : ssh_public_key vide, la paire SSH n'a pas été générée/transmise")
	}

	configureResp, err := client.Module().Configure(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: provisionResp.GetState()})
	if err != nil {
		t.Fatalf("Configure : %v", err)
	}
	if configureResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Configure().Status = %v", configureResp.GetStatus())
	}
	if call := ansibleServer.lastCall(); call == nil || !strings.Contains(string(call.GetPlaybookYaml()), "installer chrony") {
		t.Errorf("Configure n'a pas envoyé install_chrony.yml : %+v", call)
	} else if call.GetTarget().GetHost() != "10.42.0.5" {
		t.Errorf("Configure target.host = %q, attendu 10.42.0.5", call.GetTarget().GetHost())
	}

	verifyResp, err := client.Module().Verify(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: configureResp.GetState()})
	if err != nil {
		t.Fatalf("Verify : %v", err)
	}
	if verifyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Verify().Status = %v", verifyResp.GetStatus())
	}
	if call := ansibleServer.lastCall(); call == nil || !strings.Contains(string(call.GetPlaybookYaml()), "attendre une synchronisation exploitable") {
		t.Errorf("Verify n'a pas envoyé check_offset.yml : %+v", call)
	} else if call.GetVars().AsMap()["ntp_server"] != "10.42.0.5" {
		t.Errorf("Verify vars = %+v, attendu ntp_server=10.42.0.5", call.GetVars().AsMap())
	}
	// EnsureVM du vérificateur jetable, puis DeleteVM après Verify.
	if len(vmServer.calls) != 2 || vmServer.calls[1].GetName() != "chrony01-verify" {
		t.Fatalf("EnsureVM (vérificateur) appelé avec %+v, attendu name=chrony01-verify", vmServer.calls)
	}
	if _, stillThere := vmServer.vms["chrony01-verify"]; stillThere {
		t.Error("la VM de vérification n'a pas été supprimée après Verify")
	}

	destroyResp, err := client.Module().Destroy(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: configureResp.GetState()})
	if err != nil {
		t.Fatalf("Destroy : %v", err)
	}
	if destroyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Destroy().Status = %v", destroyResp.GetStatus())
	}
	if _, stillThere := vmServer.vms["chrony01"]; stillThere {
		t.Error("la VM chrony01 n'a pas été supprimée par Destroy")
	}
}

// TestChronyRejectsHighOffset vérifie que Verify échoue réellement quand
// l'écart dépasse le seuil (docs/07-mvp-modules.md : "écart < 100 ms") —
// pas une réussite silencieuse.
func TestChronyRejectsHighOffset(t *testing.T) {
	binaryPath, manifest := buildModule(t, "chrony")

	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch : %v", err)
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
		t.Fatalf("Check : %v", err)
	}
	provisionResp, err := client.Module().Provision(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Provision : %v", err)
	}
	configureResp, err := client.Module().Configure(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: provisionResp.GetState()})
	if err != nil {
		t.Fatalf("Configure : %v", err)
	}

	_, err = client.Module().Verify(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: configureResp.GetState()})
	if err == nil {
		t.Fatal("Verify a réussi avec un écart de 532 ms, attendu un échec")
	}
}

type fakeAnsibleServerWithOffset struct {
	ansiblev1.UnimplementedAnsibleServer
	offsetSeconds string
}

func (f *fakeAnsibleServerWithOffset) RunPlaybook(_ context.Context, req *ansiblev1.RunPlaybookRequest) (*ansiblev1.RunPlaybookResponse, error) {
	if strings.Contains(string(req.GetPlaybookYaml()), "attendre une synchronisation exploitable") {
		return &ansiblev1.RunPlaybookResponse{
			Ok:     true,
			Output: `"tracking.stdout": "System time     : ` + f.offsetSeconds + ` seconds slow of NTP time"`,
		}, nil
	}
	return &ansiblev1.RunPlaybookResponse{Ok: true, Output: "ok"}, nil
}
