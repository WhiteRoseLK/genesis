// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	echov1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/test/echo/v1"
)

// ForwardEcho registers an EchoServer that relays each call to conn — the
// generic test function (test.a/v1, test.b/v1...), docs/08-milestones.md M4.
func ForwardEcho(s *grpc.Server, conn *grpc.ClientConn) {
	echov1.RegisterEchoServer(s, &forwardingEcho{client: echov1.NewEchoClient(conn)})
}

type forwardingEcho struct {
	echov1.UnimplementedEchoServer
	client echov1.EchoClient
}

func (f *forwardingEcho) Call(ctx context.Context, req *echov1.CallRequest) (*echov1.CallResponse, error) {
	return f.client.Call(ctx, req)
}
