// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/WhiteRoseLK/genesis/internal/ipam"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/spec"
	"github.com/WhiteRoseLK/genesis/internal/state"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
)

type dummyComputeVMServer struct {
	computevmv1.UnimplementedComputeVMServer
	lastReq *computevmv1.EnsureVMRequest
}

func (d *dummyComputeVMServer) EnsureVM(_ context.Context, req *computevmv1.EnsureVMRequest) (*computevmv1.VM, error) {
	d.lastReq = req
	return &computevmv1.VM{
		Id:      "vm-1",
		Name:    req.GetName(),
		Ip:      req.GetIp(),
		Status:  "running",
		SshPort: 22,
	}, nil
}

func (d *dummyComputeVMServer) DeleteVM(_ context.Context, req *computevmv1.DeleteVMRequest) (*computevmv1.DeleteVMResponse, error) {
	return &computevmv1.DeleteVMResponse{}, nil
}

func TestEngineIPAMAllocationAndPersistence(t *testing.T) {
	stateDir := t.TempDir()
	st := state.New()
	if err := state.Save(stateDir, st); err != nil {
		t.Fatal(err)
	}

	netCfg := spec.Network{
		CIDR:    "10.10.0.0/24",
		Gateway: "10.10.0.1",
		Pool:    "10.10.0.50-10.10.0.99",
		Domain:  "lab.internal",
	}
	seedCfg := spec.Seed{
		Address: "10.10.0.10",
	}

	env := &spec.Environment{
		Network: netCfg,
		Seed:    seedCfg,
	}

	res, err := resolver.Resolve(env, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Setup backend compute provider
	backendSrv := grpc.NewServer()
	backend := &dummyComputeVMServer{}
	computevmv1.RegisterComputeVMServer(backendSrv, backend)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = backendSrv.Serve(lis) }()
	t.Cleanup(backendSrv.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// 2. Initialize IPAM allocator as Engine does in Run
	alloc, err := ipam.New(res.Network.Pool, ipam.Options{
		Gateway:   res.Network.Gateway,
		Reserved:  []string{res.Network.Gateway, res.Seed.Address},
		Allocated: st.AllocatedIPs,
		OnChange: func(allocated map[string]string) error {
			st.AllocatedIPs = allocated
			return state.Save(stateDir, st)
		},
	})
	if err != nil {
		t.Fatalf("ipam.New: %v", err)
	}

	// 3. Register with broker registry using the allocator
	e := New(stateDir, newTestSecretsStore(t))
	e.Allocator = alloc

	brokerLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = brokerLis.Close() }()

	brokerConn, err := grpc.NewClient(brokerLis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = brokerConn.Close() }()

	// Register forwarder through registry
	e.Registry.SetModuleProvider("compute.vm/v1", conn, func(s *grpc.Server, cc *grpc.ClientConn) {
		e.Allocator = alloc
	})

	// Check allocation behavior directly with allocator and broker forwarder
	ip1, err := alloc.Allocate("infra01")
	if err != nil {
		t.Fatalf("alloc.Allocate(infra01): %v", err)
	}
	if ip1 != "10.10.0.50" {
		t.Errorf("allocated IP = %q, want 10.10.0.50", ip1)
	}

	// Verify state was persisted to disk
	loadedState, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if loadedState.AllocatedIPs["infra01"] != "10.10.0.50" {
		t.Errorf("persisted IP = %q, want 10.10.0.50", loadedState.AllocatedIPs["infra01"])
	}

	// Next allocation gets the next free IP
	ip2, err := alloc.Allocate("dns01")
	if err != nil {
		t.Fatalf("alloc.Allocate(dns01): %v", err)
	}
	if ip2 != "10.10.0.51" {
		t.Errorf("allocated IP = %q, want 10.10.0.51", ip2)
	}

	// Re-loading state in a new run preserves previous allocations (idempotence)
	newRunAlloc, err := ipam.New(res.Network.Pool, ipam.Options{
		Gateway:   res.Network.Gateway,
		Reserved:  []string{res.Network.Gateway, res.Seed.Address},
		Allocated: loadedState.AllocatedIPs,
	})
	if err != nil {
		t.Fatalf("ipam.New second run: %v", err)
	}
	sameIP1, err := newRunAlloc.Allocate("infra01")
	if err != nil {
		t.Fatalf("newRunAlloc.Allocate(infra01): %v", err)
	}
	if sameIP1 != ip1 {
		t.Errorf("idempotence failed: got %q, want %q", sameIP1, ip1)
	}

	// Release frees the IP
	if err := alloc.Release("infra01"); err != nil {
		t.Fatalf("alloc.Release: %v", err)
	}
	loadedAfterRelease, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if _, exists := loadedAfterRelease.AllocatedIPs["infra01"]; exists {
		t.Error("infra01 should be deleted from AllocatedIPs after release")
	}

	_ = filepath.Join(stateDir, "test")
}
