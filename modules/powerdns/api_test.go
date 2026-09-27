// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
)

// newTestPDNSServer simulates the PowerDNS Authoritative REST API on the
// routes api.go really uses — it proves the exact shape of the HTTP
// requests/responses without depending on Docker (the real product is
// installed/configured by ansible, a mechanism already proven outside this
// module, internal/broker/ansible_test.go).
type testPDNSServer struct {
	zones map[string]*pdnsZoneResponse
	// lastAPIKey records the last X-API-Key header received, to prove that it
	// is passed on.
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

// TestPowerDNSZoneServerRoundTrip proves the real Upsert -> List -> Delete ->
// List round trip through real HTTP requests (httptest), including the
// automatic zone creation on the first UpsertRecord and the filtering of the
// automatically created SOA/NS records (docs/07-mvp-modules.md).
func TestPowerDNSZoneServerRoundTrip(t *testing.T) {
	ts, srv := newTestPDNSServer()
	defer srv.Close()

	zoneServer := &powerdnsZoneServer{
		client: newPDNSClient(srv.URL, "test-key"),
		vmIP:   "10.10.0.20",
		domain: "lab.internal",
	}
	ctx := context.Background()

	if _, err := zoneServer.UpsertRecord(ctx, &dnszonev1.Record{
		Zone: "lab.internal", Name: "infra01", Type: "A", Values: []string{"10.10.0.5"}, Ttl: 300,
	}); err != nil {
		t.Fatalf("UpsertRecord: %v", err)
	}
	if ts.lastAPIKey != "test-key" {
		t.Errorf("X-API-Key = %q, want the test key", ts.lastAPIKey)
	}

	records, err := zoneServer.ListRecords(ctx, &dnszonev1.Zone{Zone: "lab.internal"})
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(records.GetRecords()) != 1 {
		t.Fatalf("ListRecords = %+v, want 1 record (SOA/NS filtered out)", records.GetRecords())
	}
	got := records.GetRecords()[0]
	if got.GetName() != "infra01" || got.GetType() != "A" || got.GetValues()[0] != "10.10.0.5" {
		t.Errorf("record = %+v, want infra01 A 10.10.0.5", got)
	}

	if _, err := zoneServer.DeleteRecord(ctx, &dnszonev1.RecordKey{Zone: "lab.internal", Name: "infra01", Type: "A"}); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
	records, err = zoneServer.ListRecords(ctx, &dnszonev1.Zone{Zone: "lab.internal"})
	if err != nil {
		t.Fatalf("ListRecords after DeleteRecord: %v", err)
	}
	if len(records.GetRecords()) != 0 {
		t.Errorf("ListRecords after DeleteRecord = %+v, want empty", records.GetRecords())
	}
}

// TestEnsureZoneIdempotent proves that creating the same zone twice (a 409
// from PowerDNS) is not an error (doc 03 §4 rule 3: idempotence).
func TestEnsureZoneIdempotent(t *testing.T) {
	_, srv := newTestPDNSServer()
	defer srv.Close()
	client := newPDNSClient(srv.URL, "test-key")
	ctx := context.Background()

	if err := client.ensureZone(ctx, "lab.internal"); err != nil {
		t.Fatalf("first ensureZone: %v", err)
	}
	if err := client.ensureZone(ctx, "lab.internal"); err != nil {
		t.Fatalf("second ensureZone (already exists): %v", err)
	}
}
