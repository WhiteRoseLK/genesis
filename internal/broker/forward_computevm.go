// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
)

// Allocator manages IP allocations for compute VMs.
type Allocator interface {
	Allocate(name string) (string, error)
	Release(name string) error
	Gateway() string
}

// ForwardComputeVM registers a ComputeVMServer that relays each call to conn,
// the dispensed connection of the module currently providing compute.vm/v1
// (fake-compute in M4, proxmox from M5).
func ForwardComputeVM(s *grpc.Server, conn *grpc.ClientConn) {
	ForwardComputeVMWithAllocator(s, conn, nil)
}

// ForwardComputeVMWithAllocator registers a ComputeVMServer with an optional
// IP allocator that automatically assigns an IP address from the pool and sets
// the default gateway if not specified by the caller.
func ForwardComputeVMWithAllocator(s *grpc.Server, conn *grpc.ClientConn, alloc Allocator) {
	computevmv1.RegisterComputeVMServer(s, &forwardingComputeVM{
		client:    computevmv1.NewComputeVMClient(conn),
		allocator: alloc,
	})
}

type forwardingComputeVM struct {
	computevmv1.UnimplementedComputeVMServer
	client    computevmv1.ComputeVMClient
	allocator Allocator
}

func (f *forwardingComputeVM) EnsureImage(ctx context.Context, req *computevmv1.EnsureImageRequest) (*computevmv1.EnsureImageResponse, error) {
	return f.client.EnsureImage(ctx, req)
}

func (f *forwardingComputeVM) EnsureVM(ctx context.Context, req *computevmv1.EnsureVMRequest) (*computevmv1.VM, error) {
	if f.allocator != nil {
		if req.GetIp() == "" {
			ip, err := f.allocator.Allocate(req.GetName())
			if err != nil {
				return nil, err
			}
			req.Ip = ip
		}
		if req.GetGateway() == "" && f.allocator.Gateway() != "" {
			req.Gateway = f.allocator.Gateway()
		}
	}
	vm, err := f.client.EnsureVM(ctx, req)
	if err != nil {
		return nil, err
	}
	if vm != nil && vm.GetIp() == "" && req.GetIp() != "" {
		vm.Ip = req.GetIp()
	}
	if vm != nil && vm.GetSshHostPublicKey() == "" && req.GetSshHostPublicKey() != "" {
		vm.SshHostPublicKey = req.GetSshHostPublicKey()
	}
	return vm, nil
}

func (f *forwardingComputeVM) GetVM(ctx context.Context, req *computevmv1.GetVMRequest) (*computevmv1.VM, error) {
	return f.client.GetVM(ctx, req)
}

func (f *forwardingComputeVM) DeleteVM(ctx context.Context, req *computevmv1.DeleteVMRequest) (*computevmv1.DeleteVMResponse, error) {
	resp, err := f.client.DeleteVM(ctx, req)
	if err != nil {
		return nil, err
	}
	if f.allocator != nil {
		_ = f.allocator.Release(req.GetName())
	}
	return resp, nil
}

func (f *forwardingComputeVM) Now(ctx context.Context, req *computevmv1.NowRequest) (*computevmv1.NowResponse, error) {
	return f.client.Now(ctx, req)
}
