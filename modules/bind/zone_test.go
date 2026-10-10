// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
)

func TestBindZoneServerOperations(t *testing.T) {
	ctx := context.Background()
	server := &bindZoneServer{
		records: map[string]*dnszonev1.Record{},
		vmIP:    "10.10.0.52",
		domain:  "lab.internal",
	}

	// 1. UpsertRecord
	_, err := server.UpsertRecord(ctx, &dnszonev1.Record{
		Zone:   "lab.internal",
		Name:   "api",
		Type:   "A",
		Values: []string{"10.10.0.100"},
		Ttl:    300,
	})
	if err != nil {
		t.Fatalf("UpsertRecord: %v", err)
	}

	_, err = server.UpsertRecord(ctx, &dnszonev1.Record{
		Zone:   "lab.internal",
		Name:   "db",
		Type:   "A",
		Values: []string{"10.10.0.101"},
		Ttl:    300,
	})
	if err != nil {
		t.Fatalf("UpsertRecord: %v", err)
	}

	// 2. ListRecords
	listResp, err := server.ListRecords(ctx, &dnszonev1.Zone{Zone: "lab.internal"})
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(listResp.GetRecords()) != 2 {
		t.Fatalf("ListRecords returned %d records, want 2", len(listResp.GetRecords()))
	}

	// 2b. Verify automatic reverse PTR derivation
	revResp, err := server.ListRecords(ctx, &dnszonev1.Zone{Zone: "0.10.10.in-addr.arpa"})
	if err != nil {
		t.Fatalf("ListRecords reverse: %v", err)
	}
	if len(revResp.GetRecords()) != 2 {
		t.Fatalf("ListRecords reverse returned %d records, want 2", len(revResp.GetRecords()))
	}

	// 3. Zone file rendering (should only render forward zone records)
	server.mu.Lock()
	content := server.renderZoneFileLocked()
	server.mu.Unlock()

	if !strings.Contains(content, "$ORIGIN lab.internal.") {
		t.Errorf("zone file missing ORIGIN: %s", content)
	}
	if !strings.Contains(content, "api IN A 10.10.0.100") {
		t.Errorf("zone file missing api record: %s", content)
	}
	if !strings.Contains(content, "db IN A 10.10.0.101") {
		t.Errorf("zone file missing db record: %s", content)
	}
	if strings.Contains(content, "in-addr.arpa") || strings.Contains(content, "PTR") {
		t.Errorf("zone file should not contain reverse records: %s", content)
	}

	// 4. DeleteRecord
	_, err = server.DeleteRecord(ctx, &dnszonev1.RecordKey{
		Zone: "lab.internal",
		Name: "api",
		Type: "A",
	})
	if err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}

	listResp2, err := server.ListRecords(ctx, &dnszonev1.Zone{Zone: "lab.internal"})
	if err != nil {
		t.Fatalf("ListRecords after delete: %v", err)
	}
	if len(listResp2.GetRecords()) != 1 {
		t.Fatalf("ListRecords after delete returned %d records, want 1", len(listResp2.GetRecords()))
	}
	if listResp2.GetRecords()[0].GetName() != "db" {
		t.Errorf("remaining record name = %q, want db", listResp2.GetRecords()[0].GetName())
	}

	revResp2, err := server.ListRecords(ctx, &dnszonev1.Zone{Zone: "0.10.10.in-addr.arpa"})
	if err != nil {
		t.Fatalf("ListRecords reverse after delete: %v", err)
	}
	if len(revResp2.GetRecords()) != 1 {
		t.Fatalf("ListRecords reverse after delete returned %d records, want 1", len(revResp2.GetRecords()))
	}
	if revResp2.GetRecords()[0].GetName() != "101" {
		t.Errorf("remaining reverse record name = %q, want 101", revResp2.GetRecords()[0].GetName())
	}
}
