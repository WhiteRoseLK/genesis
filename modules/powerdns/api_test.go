// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dnszonev1 "genesis/sdk/go/gen/functions/dns/zone/v1"
)

// newTestPDNSServer simule l'API REST de PowerDNS Authoritative sur les
// routes que api.go utilise réellement — prouve la forme exacte des
// requêtes/réponses HTTP sans dépendre de Docker (le vrai produit est
// installé/configuré par ansible, mécanisme déjà prouvé en dehors de ce
// module, internal/broker/ansible_test.go).
type testPDNSServer struct {
	zones map[string]*pdnsZoneResponse
	// lastAPIKey capture le dernier en-tête X-API-Key reçu, pour prouver
	// qu'il est bien transmis.
	lastAPIKey string
}

func newTestPDNSServer() (*testPDNSServer, *httptest.Server) {
	ts := &testPDNSServer{zones: map[string]*pdnsZoneResponse{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/servers/localhost/zones", func(w http.ResponseWriter, r *http.Request) {
		ts.lastAPIKey = r.Header.Get("X-API-Key")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var create pdnsZoneCreate
		if err := json.NewDecoder(r.Body).Decode(&create); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, exists := ts.zones[create.Name]; exists {
			w.WriteHeader(http.StatusConflict)
			return
		}
		ts.zones[create.Name] = &pdnsZoneResponse{RRsets: []pdnsRRset{
			{Name: create.Name, Type: "SOA", TTL: 3600, Records: []pdnsRecordContent{{Content: "ns1." + create.Name + " admin." + create.Name + " 1 3600 900 604800 300"}}},
			{Name: create.Name, Type: "NS", TTL: 3600, Records: []pdnsRecordContent{{Content: "ns1." + create.Name}}},
		}}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/api/v1/servers/localhost/zones/", func(w http.ResponseWriter, r *http.Request) {
		ts.lastAPIKey = r.Header.Get("X-API-Key")
		zoneName := r.URL.Path[len("/api/v1/servers/localhost/zones/"):]
		zone, ok := ts.zones[zoneName]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(zone)
		case http.MethodPatch:
			var patch pdnsZonePatch
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			for _, rrset := range patch.RRsets {
				zone.RRsets = removeRRset(zone.RRsets, rrset.Name, rrset.Type)
				if rrset.ChangeType == "REPLACE" {
					zone.RRsets = append(zone.RRsets, rrset)
				}
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return ts, httptest.NewServer(mux)
}

func removeRRset(rrsets []pdnsRRset, name, typ string) []pdnsRRset {
	out := rrsets[:0]
	for _, rr := range rrsets {
		if rr.Name == name && rr.Type == typ {
			continue
		}
		out = append(out, rr)
	}
	return out
}

// TestPowerDNSZoneServerRoundTrip prouve le round-trip réel Upsert -> List
// -> Delete -> List à travers de vraies requêtes HTTP (httptest), y compris
// la création automatique de zone au premier UpsertRecord et le filtrage
// des enregistrements SOA/NS auto-créés (docs/07-modules-mvp.md).
func TestPowerDNSZoneServerRoundTrip(t *testing.T) {
	ts, srv := newTestPDNSServer()
	defer srv.Close()

	zoneServer := &powerdnsZoneServer{
		client: newPDNSClient(srv.URL, "clé-de-test"),
		vmIP:   "10.10.0.20",
		domain: "lab.internal",
	}
	ctx := context.Background()

	if _, err := zoneServer.UpsertRecord(ctx, &dnszonev1.Record{
		Zone: "lab.internal", Name: "infra01", Type: "A", Values: []string{"10.10.0.5"}, Ttl: 300,
	}); err != nil {
		t.Fatalf("UpsertRecord : %v", err)
	}
	if ts.lastAPIKey != "clé-de-test" {
		t.Errorf("X-API-Key = %q, attendu la clé de test", ts.lastAPIKey)
	}

	records, err := zoneServer.ListRecords(ctx, &dnszonev1.Zone{Zone: "lab.internal"})
	if err != nil {
		t.Fatalf("ListRecords : %v", err)
	}
	if len(records.GetRecords()) != 1 {
		t.Fatalf("ListRecords = %+v, attendu 1 enregistrement (SOA/NS filtrés)", records.GetRecords())
	}
	got := records.GetRecords()[0]
	if got.GetName() != "infra01" || got.GetType() != "A" || got.GetValues()[0] != "10.10.0.5" {
		t.Errorf("enregistrement = %+v, attendu infra01 A 10.10.0.5", got)
	}

	if _, err := zoneServer.DeleteRecord(ctx, &dnszonev1.RecordKey{Zone: "lab.internal", Name: "infra01", Type: "A"}); err != nil {
		t.Fatalf("DeleteRecord : %v", err)
	}
	records, err = zoneServer.ListRecords(ctx, &dnszonev1.Zone{Zone: "lab.internal"})
	if err != nil {
		t.Fatalf("ListRecords après DeleteRecord : %v", err)
	}
	if len(records.GetRecords()) != 0 {
		t.Errorf("ListRecords après DeleteRecord = %+v, attendu vide", records.GetRecords())
	}
}

// TestEnsureZoneIdempotent prouve que créer deux fois la même zone (409 de
// PowerDNS) n'est pas une erreur (docs03 §4 règle 3 : idempotence).
func TestEnsureZoneIdempotent(t *testing.T) {
	_, srv := newTestPDNSServer()
	defer srv.Close()
	client := newPDNSClient(srv.URL, "clé-de-test")
	ctx := context.Background()

	if err := client.ensureZone(ctx, "lab.internal"); err != nil {
		t.Fatalf("premier ensureZone : %v", err)
	}
	if err := client.ensureZone(ctx, "lab.internal"); err != nil {
		t.Fatalf("second ensureZone (déjà existante) : %v", err)
	}
}
