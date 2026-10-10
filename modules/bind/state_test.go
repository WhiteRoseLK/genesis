// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

type mockComputeVMClient struct {
	computevmv1.ComputeVMClient
	deletedName string
}

func (m *mockComputeVMClient) DeleteVM(_ context.Context, in *computevmv1.DeleteVMRequest, _ ...grpc.CallOption) (*computevmv1.DeleteVMResponse, error) {
	m.deletedName = in.GetName()
	return &computevmv1.DeleteVMResponse{}, nil
}

func TestBindCheckRestoresZoneServer(t *testing.T) {
	zoneServer := &bindZoneServer{records: map[string]*dnszonev1.Record{}}
	module := &bindModule{
		zoneServer: zoneServer,
	}

	st, err := sdk.NewState(map[string]any{
		"configured": true,
		"vm_name":    "bind01",
		"vm_ip":      "10.20.30.40",
		"domain":     "test.internal",
		"records": map[string]any{
			"test.internal|app|A": map[string]any{
				"zone":   "test.internal",
				"name":   "app",
				"type":   "A",
				"values": []any{"10.20.30.50"},
				"ttl":    float64(300),
			},
		},
	})
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}

	checkRes, err := module.Check(context.Background(), &modulev1.StepRequest{
		State: st,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if checkRes.GetStatus() != modulev1.CheckResult_STATUS_COMPLIANT {
		t.Fatalf("Check status = %v, want COMPLIANT", checkRes.GetStatus())
	}

	zoneServer.mu.Lock()
	vmIP := zoneServer.vmIP
	domain := zoneServer.domain
	recCount := len(zoneServer.records)
	zoneServer.mu.Unlock()

	if vmIP != "10.20.30.40" {
		t.Errorf("vmIP = %q, want 10.20.30.40", vmIP)
	}
	if domain != "test.internal" {
		t.Errorf("domain = %q, want test.internal", domain)
	}
	if recCount != 1 {
		t.Errorf("records count = %d, want 1", recCount)
	}

	endpoint, err := zoneServer.Endpoint(context.Background(), &dnszonev1.Empty{})
	if err != nil {
		t.Fatalf("Endpoint: %v", err)
	}
	if endpoint.GetAddress() != "10.20.30.40" || endpoint.GetPort() != 53 {
		t.Errorf("Endpoint = %+v, want Address=10.20.30.40, Port=53", endpoint)
	}
}

func TestBindDestroyClearsStateAndZoneServer(t *testing.T) {
	zoneServer := &bindZoneServer{
		records: map[string]*dnszonev1.Record{
			"test.internal|app|A": {Zone: "test.internal", Name: "app", Type: "A", Values: []string{"10.20.30.50"}},
		},
		vmIP:       "10.20.30.40",
		domain:     "test.internal",
		configured: true,
	}
	mockVM := &mockComputeVMClient{}
	module := &bindModule{
		zoneServer: zoneServer,
		vmClient:   mockVM,
	}

	st, err := sdk.NewState(map[string]any{
		"configured": true,
		"vm_name":    "bind01",
	})
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}

	res, err := module.Destroy(context.Background(), &modulev1.StepRequest{
		State: st,
	})
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if res.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Destroy status = %v, want OK", res.GetStatus())
	}
	if mockVM.deletedName != "bind01" {
		t.Errorf("DeleteVM called with %q, want bind01", mockVM.deletedName)
	}

	zoneServer.mu.Lock()
	vmIP := zoneServer.vmIP
	domain := zoneServer.domain
	configured := zoneServer.configured
	recCount := len(zoneServer.records)
	zoneServer.mu.Unlock()

	if vmIP != "" || domain != "" || configured || recCount != 0 {
		t.Errorf("zoneServer not cleared: vmIP=%q, domain=%q, configured=%v, recCount=%d", vmIP, domain, configured, recCount)
	}

	stateMap := sdk.StateMap(res.GetState())
	if len(stateMap) != 0 {
		t.Errorf("Destroy returned non-empty state: %+v", stateMap)
	}
}
