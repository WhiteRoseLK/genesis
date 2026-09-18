// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	osbasev1 "genesis/sdk/go/gen/functions/os/base/v1"
)

// ForwardOSBase enregistre un BaseServer qui relaie chaque appel vers conn,
// la connexion dispensée du module qui fournit os.base/v1 (base-os).
func ForwardOSBase(s *grpc.Server, conn *grpc.ClientConn) {
	osbasev1.RegisterBaseServer(s, &forwardingOSBase{client: osbasev1.NewBaseClient(conn)})
}

type forwardingOSBase struct {
	osbasev1.UnimplementedBaseServer
	client osbasev1.BaseClient
}

func (f *forwardingOSBase) Harden(ctx context.Context, req *osbasev1.HardenRequest) (*osbasev1.HardenResponse, error) {
	return f.client.Harden(ctx, req)
}

func (f *forwardingOSBase) TrustCA(ctx context.Context, req *osbasev1.TrustCARequest) (*osbasev1.TrustCAResponse, error) {
	return f.client.TrustCA(ctx, req)
}

func (f *forwardingOSBase) SetResolver(ctx context.Context, req *osbasev1.SetResolverRequest) (*osbasev1.SetResolverResponse, error) {
	return f.client.SetResolver(ctx, req)
}

func (f *forwardingOSBase) SetNTP(ctx context.Context, req *osbasev1.SetNTPRequest) (*osbasev1.SetNTPResponse, error) {
	return f.client.SetNTP(ctx, req)
}
