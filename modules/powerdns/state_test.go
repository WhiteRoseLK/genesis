// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	secretsv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/secrets/v1"
	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

type mockSecretsClient struct {
	secretsv1.SecretsClient
	apiKey string
}

func (m *mockSecretsClient) Get(_ context.Context, _ *secretsv1.GetRequest, _ ...grpc.CallOption) (*secretsv1.GetResponse, error) {
	return &secretsv1.GetResponse{Value: m.apiKey}, nil
}

type mockComputeVMClient struct {
	computevmv1.ComputeVMClient
	deletedName string
}

func (m *mockComputeVMClient) DeleteVM(_ context.Context, in *computevmv1.DeleteVMRequest, _ ...grpc.CallOption) (*computevmv1.DeleteVMResponse, error) {
	m.deletedName = in.GetName()
	return &computevmv1.DeleteVMResponse{}, nil
}

func TestPowerDNSCheckRestoresZoneServer(t *testing.T) {
	zoneServer := &powerdnsZoneServer{}
	module := &powerdnsModule{
		zoneServer:    zoneServer,
		secretsClient: &mockSecretsClient{apiKey: "test-api-key-xyz"},
	}

	st, err := sdk.NewState(map[string]any{
		"configured": true,
		"vm_name":    "powerdns01",
		"vm_ip":      "10.20.30.40",
		"domain":     "test.internal",
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
	client := zoneServer.client
	vmIP := zoneServer.vmIP
	domain := zoneServer.domain
	zoneServer.mu.Unlock()

	if vmIP != "10.20.30.40" {
		t.Errorf("vmIP = %q, want 10.20.30.40", vmIP)
	}
	if domain != "test.internal" {
		t.Errorf("domain = %q, want test.internal", domain)
	}
	if client == nil {
		t.Fatal("zoneServer.client was not restored")
	}

	endpoint, err := zoneServer.Endpoint(context.Background(), &dnszonev1.Empty{})
	if err != nil {
		t.Fatalf("Endpoint: %v", err)
	}
	if endpoint.GetAddress() != "10.20.30.40" || endpoint.GetPort() != 53 {
		t.Errorf("Endpoint = %+v, want Address=10.20.30.40, Port=53", endpoint)
	}
}

func TestPowerDNSDestroyClearsStateAndZoneServer(t *testing.T) {
	zoneServer := &powerdnsZoneServer{
		client: newPDNSClient("http://10.20.30.40:8081", "key"),
		vmIP:   "10.20.30.40",
		domain: "test.internal",
	}
	mockVM := &mockComputeVMClient{}
	module := &powerdnsModule{
		zoneServer: zoneServer,
		vmClient:   mockVM,
	}

	st, err := sdk.NewState(map[string]any{
		"configured": true,
		"vm_name":    "powerdns01",
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
	if mockVM.deletedName != "powerdns01" {
		t.Errorf("DeleteVM called with %q, want powerdns01", mockVM.deletedName)
	}

	zoneServer.mu.Lock()
	client := zoneServer.client
	vmIP := zoneServer.vmIP
	domain := zoneServer.domain
	zoneServer.mu.Unlock()

	if client != nil || vmIP != "" || domain != "" {
		t.Errorf("zoneServer not cleared: client=%v, vmIP=%q, domain=%q", client, vmIP, domain)
	}

	stateMap := sdk.StateMap(res.GetState())
	if len(stateMap) != 0 {
		t.Errorf("Destroy returned non-empty state: %+v", stateMap)
	}
}
