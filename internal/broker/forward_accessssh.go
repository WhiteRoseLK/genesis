// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	accesssshv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/access/ssh/v1"
)

// ForwardAccessSSH registers an AccessSSHServer that relays each call to conn,
// the dispensed connection of the module currently providing access.ssh/v1 (teleport).
func ForwardAccessSSH(s *grpc.Server, conn *grpc.ClientConn) {
	accesssshv1.RegisterAccessSSHServer(s, &forwardingAccessSSH{client: accesssshv1.NewAccessSSHClient(conn)})
}

type forwardingAccessSSH struct {
	accesssshv1.UnimplementedAccessSSHServer
	client accesssshv1.AccessSSHClient
}

func (f *forwardingAccessSSH) JumpHost(ctx context.Context, req *accesssshv1.Empty) (*accesssshv1.JumpHostInfo, error) {
	return f.client.JumpHost(ctx, req)
}

func (f *forwardingAccessSSH) SignUserKey(ctx context.Context, req *accesssshv1.SignUserKeyRequest) (*accesssshv1.SignUserKeyResponse, error) {
	return f.client.SignUserKey(ctx, req)
}
