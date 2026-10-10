// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
)

type mockComputeVMServer struct {
	computevmv1.UnimplementedComputeVMServer
	lastEnsureReq *computevmv1.EnsureVMRequest
	lastDeleteReq *computevmv1.DeleteVMRequest
}

func (m *mockComputeVMServer) EnsureVM(_ context.Context, req *computevmv1.EnsureVMRequest) (*computevmv1.VM, error) {
	m.lastEnsureReq = req
	return &computevmv1.VM{
		Id:      "vm-1",
		Name:    req.GetName(),
		Ip:      req.GetIp(),
		Status:  "running",
		SshPort: 22,
	}, nil
}

func (m *mockComputeVMServer) DeleteVM(_ context.Context, req *computevmv1.DeleteVMRequest) (*computevmv1.DeleteVMResponse, error) {
	m.lastDeleteReq = req
	return &computevmv1.DeleteVMResponse{}, nil
}

type mockAllocator struct {
	allocated map[string]string
	gateway   string
	released  []string
}

func (m *mockAllocator) Allocate(name string) (string, error) {
	if ip, ok := m.allocated[name]; ok {
		return ip, nil
	}
	ip := "10.10.0.55"
	m.allocated[name] = ip
	return ip, nil
}

func (m *mockAllocator) Release(name string) error {
	m.released = append(m.released, name)
	delete(m.allocated, name)
	return nil
}

func (m *mockAllocator) Gateway() string {
	return m.gateway
}

func TestForwardComputeVMWithAllocator(t *testing.T) {
	// 1. Mock underlying module provider
	backendServer := grpc.NewServer()
	backendMock := &mockComputeVMServer{}
	computevmv1.RegisterComputeVMServer(backendServer, backendMock)

	backendLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = backendServer.Serve(backendLis) }()
	t.Cleanup(backendServer.Stop)

	backendConn, err := grpc.NewClient(backendLis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backendConn.Close() })

	// 2. Broker forwarder with allocator
	alloc := &mockAllocator{
		allocated: make(map[string]string),
		gateway:   "10.10.0.1",
	}

	brokerServer := grpc.NewServer()
	ForwardComputeVMWithAllocator(brokerServer, backendConn, alloc)

	brokerLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = brokerServer.Serve(brokerLis) }()
	t.Cleanup(brokerServer.Stop)

	brokerConn, err := grpc.NewClient(brokerLis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brokerConn.Close() })

	client := computevmv1.NewComputeVMClient(brokerConn)
	ctx := context.Background()

	// EnsureVM with empty IP should have IP and Gateway injected
	vm, err := client.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name: "infra01",
		Env:  "lab",
	})
	if err != nil {
		t.Fatalf("EnsureVM: %v", err)
	}
	if vm.GetIp() != "10.10.0.55" {
		t.Errorf("EnsureVM VM.Ip = %q, want 10.10.0.55", vm.GetIp())
	}
	if backendMock.lastEnsureReq.GetIp() != "10.10.0.55" {
		t.Errorf("backend received IP = %q, want 10.10.0.55", backendMock.lastEnsureReq.GetIp())
	}
	if backendMock.lastEnsureReq.GetGateway() != "10.10.0.1" {
		t.Errorf("backend received Gateway = %q, want 10.10.0.1", backendMock.lastEnsureReq.GetGateway())
	}

	// DeleteVM should trigger release on allocator
	_, err = client.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: "infra01"})
	if err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if len(alloc.released) != 1 || alloc.released[0] != "infra01" {
		t.Errorf("allocator released = %v, want [infra01]", alloc.released)
	}
}
