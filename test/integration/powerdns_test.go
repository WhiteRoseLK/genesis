// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
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

// powerdnsAPIPort must match the unexported constant pdnsAPIPort of
// modules/powerdns/main.go (8081): the two packages are separate binaries
// (modules/* never imports internal/, and vice versa), hence the intentional
// duplication here.
const powerdnsAPIPort = 8081

// --- simulated compute.vm/v1 ----------------------------------------------

type powerdnsFakeComputeVMServer struct {
	computevmv1.UnimplementedComputeVMServer
	mu    sync.Mutex
	vms   map[string]*computevmv1.VM
	calls []*computevmv1.EnsureVMRequest
}

func newPowerdnsFakeComputeVMServer() *powerdnsFakeComputeVMServer {
	return &powerdnsFakeComputeVMServer{vms: map[string]*computevmv1.VM{}}
}

func (f *powerdnsFakeComputeVMServer) EnsureVM(_ context.Context, req *computevmv1.EnsureVMRequest) (*computevmv1.VM, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if vm, ok := f.vms[req.GetName()]; ok {
		return vm, nil
	}
	// 127.0.0.1: the "target" VM must really reach the fake PowerDNS HTTP
	// server started by this test on the loopback.
	vm := &computevmv1.VM{Id: "vm-" + req.GetName(), Name: req.GetName(), Ip: "127.0.0.1", Status: "running", SshPort: 22}
	f.vms[req.GetName()] = vm
	return vm, nil
}

func (f *powerdnsFakeComputeVMServer) DeleteVM(_ context.Context, req *computevmv1.DeleteVMRequest) (*computevmv1.DeleteVMResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.vms, req.GetName())
	return &computevmv1.DeleteVMResponse{}, nil
}

// --- simulated os.base/v1 --------------------------------------------------

type powerdnsFakeOSBaseServer struct {
	osbasev1.UnimplementedBaseServer
	mu    sync.Mutex
	calls []string
}

func (f *powerdnsFakeOSBaseServer) SetNTP(_ context.Context, req *osbasev1.SetNTPRequest) (*osbasev1.SetNTPResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "SetNTP:"+strings.Join(req.GetServers(), ","))
	f.mu.Unlock()
	return &osbasev1.SetNTPResponse{}, nil
}

func (f *powerdnsFakeOSBaseServer) SetResolver(_ context.Context, req *osbasev1.SetResolverRequest) (*osbasev1.SetResolverResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "SetResolver:"+strings.Join(req.GetNameservers(), ","))
	f.mu.Unlock()
	return &osbasev1.SetResolverResponse{}, nil
}

// --- simulated core.ansible/v1 ---------------------------------------------

type powerdnsFakeAnsibleServer struct {
	ansiblev1.UnimplementedAnsibleServer
	mu    sync.Mutex
	calls []*ansiblev1.RunPlaybookRequest
}

func (f *powerdnsFakeAnsibleServer) RunPlaybook(_ context.Context, req *ansiblev1.RunPlaybookRequest) (*ansiblev1.RunPlaybookResponse, error) {
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
        "forward": "10.255.255.1",
        "reverse": "verify-probe.lab.internal."
    }
}`,
		}, nil
	}
	return &ansiblev1.RunPlaybookResponse{Ok: true, Output: "ok"}, nil
}

func (f *powerdnsFakeAnsibleServer) lastCall() *ansiblev1.RunPlaybookRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

// --- simulated time.ntp/v1 and dns.resolver/v1 (active providers before the
// handover, e.g. chrony and coredns) ---------------------------------------

type powerdnsFakeTimeNTPServer struct {
	timentpv1.UnimplementedTimeNTPServer
}

func (powerdnsFakeTimeNTPServer) Endpoint(context.Context, *timentpv1.Empty) (*timentpv1.EndpointInfo, error) {
	return &timentpv1.EndpointInfo{Address: "10.10.0.9", Port: 123}, nil
}

type powerdnsFakeDnsResolverServer struct {
	dnsresolverv1.UnimplementedDnsResolverServer
}

func (powerdnsFakeDnsResolverServer) Endpoint(context.Context, *dnsresolverv1.Empty) (*dnsresolverv1.EndpointInfo, error) {
	return &dnsresolverv1.EndpointInfo{Address: "10.10.0.8", Port: 53}, nil
}

// --- simulated dns.zone/v1@seed (coredns) ----------------------------------

type powerdnsFakeDnsZoneSeedServer struct {
	dnszonev1.UnimplementedDnsZoneServer
	records []*dnszonev1.Record
}

func (f *powerdnsFakeDnsZoneSeedServer) ListRecords(_ context.Context, req *dnszonev1.Zone) (*dnszonev1.Records, error) {
	var out []*dnszonev1.Record
	for _, r := range f.records {
		if r.GetZone() == req.GetZone() {
			out = append(out, r)
		}
	}
	return &dnszonev1.Records{Records: out}, nil
}

// --- simulated PowerDNS REST API -------------------------------------------

// startFakePowerDNSAPI simulates the PowerDNS Authoritative REST API on the
// fixed port modules/powerdns expects (127.0.0.1:8081) — it is the module's
// REAL HTTP client (api.go) that talks to this server, no mock on the Go side.
type fakeRRData struct {
	Values []string
	TTL    uint32
}

type fakePowerDNSAPI struct {
	mu    sync.Mutex
	zones map[string]map[string]map[string]fakeRRData // zone -> name -> type -> data
}

func startFakePowerDNSAPI(t *testing.T) *fakePowerDNSAPI {
	t.Helper()
	api := &fakePowerDNSAPI{zones: map[string]map[string]map[string]fakeRRData{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/servers/localhost/zones", func(w http.ResponseWriter, r *http.Request) {
		var create struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&create)
		api.mu.Lock()
		_, exists := api.zones[create.Name]
		if !exists {
			api.zones[create.Name] = map[string]map[string]fakeRRData{}
		}
		api.mu.Unlock()
		if exists {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/api/v1/servers/localhost/zones/", func(w http.ResponseWriter, r *http.Request) {
		zoneName := r.URL.Path[len("/api/v1/servers/localhost/zones/"):]
		api.mu.Lock()
		defer api.mu.Unlock()
		zone, ok := api.zones[zoneName]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			var rrsets []map[string]any
			for name, byType := range zone {
				for typ, data := range byType {
					recs := make([]map[string]any, 0, len(data.Values))
					for _, v := range data.Values {
						recs = append(recs, map[string]any{"content": v})
					}
					rrsets = append(rrsets, map[string]any{"name": name, "type": typ, "ttl": data.TTL, "records": recs})
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"rrsets": rrsets})
		case http.MethodPatch:
			var patch struct {
				RRsets []struct {
					Name       string `json:"name"`
					Type       string `json:"type"`
					TTL        uint32 `json:"ttl"`
					ChangeType string `json:"changetype"`
					Records    []struct {
						Content string `json:"content"`
					} `json:"records"`
				} `json:"rrsets"`
			}
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for _, rrset := range patch.RRsets {
				if zone[rrset.Name] == nil {
					zone[rrset.Name] = map[string]fakeRRData{}
				}
				delete(zone[rrset.Name], rrset.Type)
				if rrset.ChangeType == "REPLACE" {
					values := make([]string, 0, len(rrset.Records))
					for _, r := range rrset.Records {
						values = append(values, r.Content)
					}
					zone[rrset.Name][rrset.Type] = fakeRRData{Values: values, TTL: rrset.TTL}
				}
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(powerdnsAPIPort))
	if err != nil {
		t.Skipf("port %d unavailable for the fake PowerDNS API: %v", powerdnsAPIPort, err)
	}
	server := httptest.NewUnstartedServer(mux)
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return api
}

// TestPowerDNSLifecycle exercises Provision -> Configure -> Handover -> Verify
// -> Destroy with simulated
// compute.vm/os.base/ansible/time.ntp/dns.resolver/dns.zone@seed (doc 03 §4
// rule 7), real secrets (age+file, the same precaution as chrony), and a real
// simulated PowerDNS HTTP API on 127.0.0.1:8081 — the real
// generation/configuration of PowerDNS by ansible is already proven for real
// elsewhere (internal/broker/ansible_test.go), and the api.go HTTP mechanism
// in modules/powerdns/api_test.go; this test proves the lifecycle wiring and
// the handover.
func TestPowerDNSLifecycle(t *testing.T) {
	binaryPath, manifest := buildModule(t, "powerdns")
	pdnsAPI := startFakePowerDNSAPI(t)

	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer client.Close()
	ctx := context.Background()

	vmServer := newPowerdnsFakeComputeVMServer()
	osBaseServer := &powerdnsFakeOSBaseServer{}
	ansibleServer := &powerdnsFakeAnsibleServer{}
	seedServer := &powerdnsFakeDnsZoneSeedServer{records: []*dnszonev1.Record{
		{Zone: "lab.internal", Name: "infra01", Type: "A", Values: []string{"10.10.0.5"}, Ttl: 300},
	}}
	store := newTestSecretsStore(t)

	sessionID := client.Broker().NextId()
	go client.Broker().AcceptAndServe(sessionID, func(opts []grpc.ServerOption) *grpc.Server {
		s := grpc.NewServer(opts...)
		computevmv1.RegisterComputeVMServer(s, vmServer)
		osbasev1.RegisterBaseServer(s, osBaseServer)
		ansiblev1.RegisterAnsibleServer(s, ansibleServer)
		timentpv1.RegisterTimeNTPServer(s, powerdnsFakeTimeNTPServer{})
		dnsresolverv1.RegisterDnsResolverServer(s, powerdnsFakeDnsResolverServer{})
		dnszonev1.RegisterDnsZoneServer(s, seedServer)
		broker.NativeSecrets(store)("powerdns")(s)
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
	if len(vmServer.calls) != 1 || vmServer.calls[0].GetName() != "powerdns01" {
		t.Fatalf("EnsureVM called with %+v, want name=powerdns01", vmServer.calls)
	}

	configureResp, err := client.Module().Configure(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: provisionResp.GetState()})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if call := ansibleServer.lastCall(); call == nil || !strings.Contains(string(call.GetPlaybookYaml()), "PowerDNS") {
		t.Errorf("Configure did not send install_powerdns.yml: %+v", call)
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
	if cfgState["vm_ip"] != "127.0.0.1" {
		t.Errorf("configureResp vm_ip = %v, want 127.0.0.1", cfgState["vm_ip"])
	}

	handoverResp, err := client.Module().Handover(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: configureResp.GetState()})
	if err != nil {
		t.Fatalf("Handover: %v", err)
	}
	hoState := sdk.StateMap(handoverResp.GetState())
	if !hoState["handed_over"].(bool) || !hoState["configured"].(bool) {
		t.Errorf("handoverResp state = %+v, want handed_over and configured", hoState)
	}
	pdnsAPI.mu.Lock()
	got := pdnsAPI.zones["lab.internal."]["infra01.lab.internal."]["A"]
	pdnsAPI.mu.Unlock()
	if len(got.Values) != 1 || got.Values[0] != "10.10.0.5" {
		t.Errorf("after Handover, the fake PowerDNS API holds %+v for infra01.lab.internal. A, want [10.10.0.5]", got)
	}

	verifyResp, err := client.Module().Verify(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: handoverResp.GetState()})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if verifyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Verify().Status = %v", verifyResp.GetStatus())
	}
	if len(vmServer.calls) != 2 || vmServer.calls[1].GetName() != "powerdns01-verify" {
		t.Fatalf("EnsureVM (verifier) called with %+v, want name=powerdns01-verify", vmServer.calls)
	}
	if _, stillThere := vmServer.vms["powerdns01-verify"]; stillThere {
		t.Error("the verification VM was not deleted after Verify")
	}
	// Verify's probe must be cleaned up, the real data taken over (infra01)
	// must remain.
	pdnsAPI.mu.Lock()
	_, probeStillThere := pdnsAPI.zones["lab.internal."]["verify-probe.lab.internal."]["A"]
	_, infraStillThere := pdnsAPI.zones["lab.internal."]["infra01.lab.internal."]["A"]
	pdnsAPI.mu.Unlock()
	if probeStillThere {
		t.Error("Verify's probe record was not cleaned up")
	}
	if !infraStillThere {
		t.Error("Verify deleted a taken-over record that did not belong to it")
	}

	destroyResp, err := client.Module().Destroy(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: handoverResp.GetState()})
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if destroyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Destroy().Status = %v", destroyResp.GetStatus())
	}
	if _, stillThere := vmServer.vms["powerdns01"]; stillThere {
		t.Error("the powerdns01 VM was not deleted by Destroy")
	}
	if len(sdk.StateMap(destroyResp.GetState())) != 0 {
		t.Errorf("Destroy returned non-empty state: %+v", sdk.StateMap(destroyResp.GetState()))
	}
}
