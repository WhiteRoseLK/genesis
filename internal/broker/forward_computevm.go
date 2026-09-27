// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
)

// ForwardComputeVM registers a ComputeVMServer that relays each call to conn,
// the dispensed connection of the module currently providing compute.vm/v1
// (fake-compute in M4, proxmox from M5).
func ForwardComputeVM(s *grpc.Server, conn *grpc.ClientConn) {
	computevmv1.RegisterComputeVMServer(s, &forwardingComputeVM{client: computevmv1.NewComputeVMClient(conn)})
}

type forwardingComputeVM struct {
	computevmv1.UnimplementedComputeVMServer
	client computevmv1.ComputeVMClient
}

func (f *forwardingComputeVM) EnsureImage(ctx context.Context, req *computevmv1.EnsureImageRequest) (*computevmv1.EnsureImageResponse, error) {
	return f.client.EnsureImage(ctx, req)
}

func (f *forwardingComputeVM) EnsureVM(ctx context.Context, req *computevmv1.EnsureVMRequest) (*computevmv1.VM, error) {
	return f.client.EnsureVM(ctx, req)
}

func (f *forwardingComputeVM) GetVM(ctx context.Context, req *computevmv1.GetVMRequest) (*computevmv1.VM, error) {
	return f.client.GetVM(ctx, req)
}

func (f *forwardingComputeVM) DeleteVM(ctx context.Context, req *computevmv1.DeleteVMRequest) (*computevmv1.DeleteVMResponse, error) {
	return f.client.DeleteVM(ctx, req)
}

func (f *forwardingComputeVM) Now(ctx context.Context, req *computevmv1.NowRequest) (*computevmv1.NowResponse, error) {
	return f.client.Now(ctx, req)
}
