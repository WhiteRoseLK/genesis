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
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	dnsresolverv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/resolver/v1"
	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
	osbasev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// powerdnsAPIPort doit correspondre à la constante non exportée
// pdnsAPIPort de modules/powerdns/main.go (8081) : les deux packages sont
// des binaires séparés (modules/* n'importe jamais internal/, et
// inversement), donc dupliquée ici intentionnellement.
const powerdnsAPIPort = 8081

// --- compute.vm/v1 simulé --------------------------------------------------

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
	// 127.0.0.1 : la VM "cible" doit réellement joindre le faux serveur
	// PowerDNS HTTP lancé par ce test sur la boucle locale.
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

// --- os.base/v1 simulé ------------------------------------------------------

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

// --- core.ansible/v1 simulé --------------------------------------------------

type powerdnsFakeAnsibleServer struct {
	ansiblev1.UnimplementedAnsibleServer
	mu    sync.Mutex
	calls []*ansiblev1.RunPlaybookRequest
}

func (f *powerdnsFakeAnsibleServer) RunPlaybook(_ context.Context, req *ansiblev1.RunPlaybookRequest) (*ansiblev1.RunPlaybookResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()

	if strings.Contains(string(req.GetPlaybookYaml()), "installer les outils DNS de vérification") {
		return &ansiblev1.RunPlaybookResponse{
			Ok: true,
			Output: `TASK [afficher les résultats] ***
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

// --- time.ntp/v1 et dns.resolver/v1 simulés (fournisseurs actifs avant la
// passation, ex. chrony et coredns) -----------------------------------------

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

// --- dns.zone/v1@seed simulé (coredns) --------------------------------------

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

// --- API REST PowerDNS simulée -----------------------------------------------

// startFakePowerDNSAPI simule l'API REST de PowerDNS Authoritative sur le
// port fixe attendu par modules/powerdns (127.0.0.1:8081) — c'est le VRAI
// client HTTP du module (api.go) qui parle à ce serveur, aucun mock côté Go.
type fakeRRData struct {
	Values []string
	TTL    uint32
}

type fakePowerDNSAPI struct {
	mu    sync.Mutex
	zones map[string]map[string]map[string]fakeRRData // zone -> name -> type -> données
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
		t.Skipf("port %d indisponible pour la fausse API PowerDNS : %v", powerdnsAPIPort, err)
	}
	server := httptest.NewUnstartedServer(mux)
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return api
}

// TestPowerDNSLifecycle exerce Provision -> Configure -> Handover -> Verify
// -> Destroy avec compute.vm/os.base/ansible/time.ntp/dns.resolver/
// dns.zone@seed simulés (docs03 §4 règle 7), secrets réels (age+fichier,
// même précaution que chrony), et une vraie API PowerDNS HTTP simulée sur
// 127.0.0.1:8081 — la génération/configuration réelle de PowerDNS par
// ansible est déjà prouvée pour de vrai ailleurs
// (internal/broker/ansible_test.go), le mécanisme HTTP api.go l'est dans
// modules/powerdns/api_test.go ; ce test-ci prouve le câblage du cycle de
// vie et la passation.
func TestPowerDNSLifecycle(t *testing.T) {
	binaryPath, manifest := buildModule(t, "powerdns")
	pdnsAPI := startFakePowerDNSAPI(t)

	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch : %v", err)
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
		t.Fatalf("Check : %v", err)
	}

	provisionResp, err := client.Module().Provision(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Provision : %v", err)
	}
	if len(vmServer.calls) != 1 || vmServer.calls[0].GetName() != "powerdns01" {
		t.Fatalf("EnsureVM appelé avec %+v, attendu name=powerdns01", vmServer.calls)
	}

	configureResp, err := client.Module().Configure(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: provisionResp.GetState()})
	if err != nil {
		t.Fatalf("Configure : %v", err)
	}
	if call := ansibleServer.lastCall(); call == nil || !strings.Contains(string(call.GetPlaybookYaml()), "PowerDNS") {
		t.Errorf("Configure n'a pas envoyé install_powerdns.yml : %+v", call)
	}
	osBaseServer.mu.Lock()
	calls := append([]string(nil), osBaseServer.calls...)
	osBaseServer.mu.Unlock()
	if len(calls) != 2 || !strings.Contains(calls[0], "10.10.0.9") || !strings.Contains(calls[1], "10.10.0.8") {
		t.Errorf("appels os.base = %v, attendu SetNTP(10.10.0.9) puis SetResolver(10.10.0.8)", calls)
	}

	handoverResp, err := client.Module().Handover(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: configureResp.GetState()})
	if err != nil {
		t.Fatalf("Handover : %v", err)
	}
	pdnsAPI.mu.Lock()
	got := pdnsAPI.zones["lab.internal."]["infra01.lab.internal."]["A"]
	pdnsAPI.mu.Unlock()
	if len(got.Values) != 1 || got.Values[0] != "10.10.0.5" {
		t.Errorf("après Handover, la fausse API PowerDNS contient %+v pour infra01.lab.internal. A, attendu [10.10.0.5]", got)
	}

	verifyResp, err := client.Module().Verify(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: handoverResp.GetState()})
	if err != nil {
		t.Fatalf("Verify : %v", err)
	}
	if verifyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Verify().Status = %v", verifyResp.GetStatus())
	}
	if len(vmServer.calls) != 2 || vmServer.calls[1].GetName() != "powerdns01-verify" {
		t.Fatalf("EnsureVM (vérificateur) appelé avec %+v, attendu name=powerdns01-verify", vmServer.calls)
	}
	if _, stillThere := vmServer.vms["powerdns01-verify"]; stillThere {
		t.Error("la VM de vérification n'a pas été supprimée après Verify")
	}
	// La sonde de Verify doit être nettoyée, la vraie donnée reprise (infra01) doit rester.
	pdnsAPI.mu.Lock()
	_, probeStillThere := pdnsAPI.zones["lab.internal."]["verify-probe.lab.internal."]["A"]
	_, infraStillThere := pdnsAPI.zones["lab.internal."]["infra01.lab.internal."]["A"]
	pdnsAPI.mu.Unlock()
	if probeStillThere {
		t.Error("l'enregistrement de sonde de Verify n'a pas été nettoyé")
	}
	if !infraStillThere {
		t.Error("Verify a supprimé un enregistrement repris qui ne lui appartenait pas")
	}

	destroyResp, err := client.Module().Destroy(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: handoverResp.GetState()})
	if err != nil {
		t.Fatalf("Destroy : %v", err)
	}
	if destroyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Destroy().Status = %v", destroyResp.GetStatus())
	}
	if _, stillThere := vmServer.vms["powerdns01"]; stillThere {
		t.Error("la VM powerdns01 n'a pas été supprimée par Destroy")
	}
}
