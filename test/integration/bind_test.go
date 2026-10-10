// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"

	"github.com/WhiteRoseLK/genesis/internal/broker"
	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	dnsresolverv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/resolver/v1"
	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
	osbasev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// --- simulated dependencies for bind --------------------------------------

type bindFakeComputeVMServer struct {
	computevmv1.UnimplementedComputeVMServer
	mu    sync.Mutex
	vms   map[string]*computevmv1.VM
	calls []*computevmv1.EnsureVMRequest
}

func newBindFakeComputeVMServer() *bindFakeComputeVMServer {
	return &bindFakeComputeVMServer{vms: map[string]*computevmv1.VM{}}
}

func (f *bindFakeComputeVMServer) EnsureVM(_ context.Context, req *computevmv1.EnsureVMRequest) (*computevmv1.VM, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if vm, ok := f.vms[req.GetName()]; ok {
		return vm, nil
	}
	vm := &computevmv1.VM{Id: "vm-" + req.GetName(), Name: req.GetName(), Ip: "10.10.0.52", Status: "running", SshPort: 22}
	f.vms[req.GetName()] = vm
	return vm, nil
}

func (f *bindFakeComputeVMServer) DeleteVM(_ context.Context, req *computevmv1.DeleteVMRequest) (*computevmv1.DeleteVMResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.vms, req.GetName())
	return &computevmv1.DeleteVMResponse{}, nil
}

type bindFakeOSBaseServer struct {
	osbasev1.UnimplementedBaseServer
	mu    sync.Mutex
	calls []string
}

func (f *bindFakeOSBaseServer) SetNTP(_ context.Context, req *osbasev1.SetNTPRequest) (*osbasev1.SetNTPResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "SetNTP:"+strings.Join(req.GetServers(), ","))
	f.mu.Unlock()
	return &osbasev1.SetNTPResponse{}, nil
}

func (f *bindFakeOSBaseServer) SetResolver(_ context.Context, req *osbasev1.SetResolverRequest) (*osbasev1.SetResolverResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "SetResolver:"+strings.Join(req.GetNameservers(), ","))
	f.mu.Unlock()
	return &osbasev1.SetResolverResponse{}, nil
}

type bindFakeAnsibleServer struct {
	ansiblev1.UnimplementedAnsibleServer
	mu    sync.Mutex
	calls []*ansiblev1.RunPlaybookRequest
}

func (f *bindFakeAnsibleServer) RunPlaybook(_ context.Context, req *ansiblev1.RunPlaybookRequest) (*ansiblev1.RunPlaybookResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()

	if strings.Contains(string(req.GetPlaybookYaml()), "install the DNS verification tools") {
		return &ansiblev1.RunPlaybookResponse{
			Ok: true,
			Output: `TASK [show the results] ***
ok: [target] => {
    "msg": {
        "external": "93.184.216.34",
        "forward": "10.255.255.1"
    }
}`,
		}, nil
	}
	return &ansiblev1.RunPlaybookResponse{Ok: true, Output: "ok"}, nil
}

func (f *bindFakeAnsibleServer) lastCall() *ansiblev1.RunPlaybookRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

type bindFakeTimeNTPServer struct {
	timentpv1.UnimplementedTimeNTPServer
}

func (bindFakeTimeNTPServer) Endpoint(context.Context, *timentpv1.Empty) (*timentpv1.EndpointInfo, error) {
	return &timentpv1.EndpointInfo{Address: "10.10.0.9", Port: 123}, nil
}

type bindFakeDnsResolverServer struct {
	dnsresolverv1.UnimplementedDnsResolverServer
}

func (bindFakeDnsResolverServer) Endpoint(context.Context, *dnsresolverv1.Empty) (*dnsresolverv1.EndpointInfo, error) {
	return &dnsresolverv1.EndpointInfo{Address: "10.10.0.8", Port: 53}, nil
}

type bindFakeDnsZoneSeedServer struct {
	dnszonev1.UnimplementedDnsZoneServer
	records []*dnszonev1.Record
}

func (s *bindFakeDnsZoneSeedServer) ListRecords(_ context.Context, req *dnszonev1.Zone) (*dnszonev1.Records, error) {
	var out []*dnszonev1.Record
	for _, r := range s.records {
		if r.GetZone() == req.GetZone() {
			out = append(out, r)
		}
	}
	return &dnszonev1.Records{Records: out}, nil
}

// TestBindLifecycle exercises Check -> Provision -> Configure -> Handover ->
// Verify -> Destroy on the bind module with simulated required functions.
func TestBindLifecycle(t *testing.T) {
	binaryPath, manifest := buildModule(t, "bind")

	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer client.Close()
	ctx := context.Background()

	vmServer := newBindFakeComputeVMServer()
	osBaseServer := &bindFakeOSBaseServer{}
	ansibleServer := &bindFakeAnsibleServer{}
	seedServer := &bindFakeDnsZoneSeedServer{records: []*dnszonev1.Record{
		{Zone: "lab.internal", Name: "infra01", Type: "A", Values: []string{"10.10.0.5"}, Ttl: 300},
	}}
	store := newTestSecretsStore(t)

	sessionID := client.Broker().NextId()
	go client.Broker().AcceptAndServe(sessionID, func(opts []grpc.ServerOption) *grpc.Server {
		s := grpc.NewServer(opts...)
		computevmv1.RegisterComputeVMServer(s, vmServer)
		osbasev1.RegisterBaseServer(s, osBaseServer)
		ansiblev1.RegisterAnsibleServer(s, ansibleServer)
		timentpv1.RegisterTimeNTPServer(s, bindFakeTimeNTPServer{})
		dnsresolverv1.RegisterDnsResolverServer(s, bindFakeDnsResolverServer{})
		dnszonev1.RegisterDnsZoneServer(s, seedServer)
		broker.NativeSecrets(store)("bind")(s)
		return s
	})
	token := strconv.FormatUint(uint64(sessionID), 10)

	// 1. Check before anything is done -> STATUS_TODO
	checkResp, err := client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if checkResp.GetStatus() != modulev1.CheckResult_STATUS_TODO {
		t.Fatalf("Check status = %v, want TODO", checkResp.GetStatus())
	}

	// 2. Provision -> creates bind01 VM
	provisionResp, err := client.Module().Provision(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(vmServer.calls) != 1 || vmServer.calls[0].GetName() != "bind01" {
		t.Fatalf("EnsureVM called with %+v, want name=bind01", vmServer.calls)
	}

	// 3. Configure -> runs install_bind.yml, sets NTP and Resolver
	configureResp, err := client.Module().Configure(ctx, &modulev1.StepRequest{
		RunId: "test", BrokerToken: token, State: provisionResp.GetState(),
	})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if call := ansibleServer.lastCall(); call == nil || !strings.Contains(string(call.GetPlaybookYaml()), "BIND 9") {
		t.Errorf("Configure did not send install_bind.yml: %+v", call)
	}
	osBaseServer.mu.Lock()
	calls := append([]string(nil), osBaseServer.calls...)
	osBaseServer.mu.Unlock()
	if len(calls) != 2 || !strings.Contains(calls[0], "10.10.0.9") || !strings.Contains(calls[1], "10.10.0.8") {
		t.Errorf("os.base calls = %v, want SetNTP(10.10.0.9) then SetResolver(10.10.0.8)", calls)
	}

	cfgState := sdk.StateMap(configureResp.GetState())
	if !cfgState["configured"].(bool) {
		t.Error("configureResp missing configured=true")
	}
	if cfgState["vm_ip"] != "10.10.0.52" {
		t.Errorf("configureResp vm_ip = %v, want 10.10.0.52", cfgState["vm_ip"])
	}

	// 4. Handover -> imports records from seed CoreDNS
	handoverResp, err := client.Module().Handover(ctx, &modulev1.StepRequest{
		RunId: "test", BrokerToken: token, State: configureResp.GetState(),
	})
	if err != nil {
		t.Fatalf("Handover: %v", err)
	}
	hoState := sdk.StateMap(handoverResp.GetState())
	if !hoState["handed_over"].(bool) || !hoState["configured"].(bool) {
		t.Errorf("handoverResp state = %+v, want handed_over and configured", hoState)
	}

	// 5. Verify -> creates verifier VM, checks resolution, deletes verifier VM
	verifyResp, err := client.Module().Verify(ctx, &modulev1.StepRequest{
		RunId: "test", BrokerToken: token, State: handoverResp.GetState(),
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verifyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Verify().Status = %v", verifyResp.GetStatus())
	}
	if len(vmServer.calls) != 2 || vmServer.calls[1].GetName() != "bind01-verify" {
		t.Fatalf("EnsureVM (verifier) called with %+v, want name=bind01-verify", vmServer.calls)
	}
	if _, stillThere := vmServer.vms["bind01-verify"]; stillThere {
		t.Error("the verification VM was not deleted after Verify")
	}

	// 6. Dispense dns.zone/v1 function and query ListRecords
	zoneConn, err := client.DispenseFunction("dns.zone/v1")
	if err != nil {
		t.Fatalf("DispenseFunction(dns.zone/v1): %v", err)
	}
	zoneClient := dnszonev1.NewDnsZoneClient(zoneConn)
	listRecordsResp, err := zoneClient.ListRecords(ctx, &dnszonev1.Zone{Zone: "lab.internal"})
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	foundInfra01 := false
	for _, rec := range listRecordsResp.GetRecords() {
		if rec.GetName() == "infra01" && len(rec.GetValues()) > 0 && rec.GetValues()[0] == "10.10.0.5" {
			foundInfra01 = true
			break
		}
	}
	if !foundInfra01 {
		t.Errorf("ListRecords did not return handed-over record infra01: %+v", listRecordsResp.GetRecords())
	}

	// 7. Endpoint
	endpoint, err := zoneClient.Endpoint(ctx, &dnszonev1.Empty{})
	if err != nil {
		t.Fatalf("Endpoint: %v", err)
	}
	if endpoint.GetAddress() != "10.10.0.52" || endpoint.GetPort() != 53 {
		t.Errorf("Endpoint = %+v, want Address=10.10.0.52, Port=53", endpoint)
	}

	// 8. Destroy -> deletes bind01 VM
	destroyResp, err := client.Module().Destroy(ctx, &modulev1.StepRequest{
		RunId: "test", BrokerToken: token, State: handoverResp.GetState(),
	})
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if destroyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Destroy().Status = %v", destroyResp.GetStatus())
	}
	if _, stillThere := vmServer.vms["bind01"]; stillThere {
		t.Error("the bind01 VM was not deleted by Destroy")
	}
	if len(sdk.StateMap(destroyResp.GetState())) != 0 {
		t.Errorf("Destroy returned non-empty state: %+v", sdk.StateMap(destroyResp.GetState()))
	}
}
