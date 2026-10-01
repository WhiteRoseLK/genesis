// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

func TestCoreDNSCheckRestoresContainerState(t *testing.T) {
	module := &coreDNSModule{}
	zoneServer := &dnsZoneServer{module: module}
	module.zoneServer = zoneServer

	st, err := sdk.NewState(map[string]any{
		"seeded":       true,
		"container_id": "test-container-id-123",
		"container_ip": "172.17.0.22",
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
	cid := zoneServer.containerID
	ip := zoneServer.ip
	zoneServer.mu.Unlock()

	if cid != "test-container-id-123" {
		t.Errorf("zoneServer.containerID = %q, want test-container-id-123", cid)
	}
	if ip != "172.17.0.22" {
		t.Errorf("zoneServer.ip = %q, want 172.17.0.22", ip)
	}

	endpoint, err := zoneServer.Endpoint(context.Background(), &dnszonev1.Empty{})
	if err != nil {
		t.Fatalf("Endpoint: %v", err)
	}
	if endpoint.GetAddress() != "172.17.0.22" || endpoint.GetPort() != 53 {
		t.Errorf("Endpoint = %+v, want Address=172.17.0.22, Port=53", endpoint)
	}
}

func TestCoreDNSSetFlagPreservesContainerState(t *testing.T) {
	module := &coreDNSModule{}
	zoneServer := &dnsZoneServer{
		module:      module,
		containerID: "cid-456",
		ip:          "172.17.0.45",
	}
	module.zoneServer = zoneServer

	res, err := module.setFlag(&modulev1.StepRequest{}, "seeded")
	if err != nil {
		t.Fatalf("setFlag: %v", err)
	}
	if res.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("setFlag status = %v, want OK", res.GetStatus())
	}

	stateMap := sdk.StateMap(res.GetState())
	if !boolFlag(stateMap, "seeded") {
		t.Error("seeded flag not set")
	}
	if stateMap["container_id"] != "cid-456" {
		t.Errorf("container_id = %v, want cid-456", stateMap["container_id"])
	}
	if stateMap["container_ip"] != "172.17.0.45" {
		t.Errorf("container_ip = %v, want 172.17.0.45", stateMap["container_ip"])
	}
}
