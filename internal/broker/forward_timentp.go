// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
)

// ForwardTimeNTP registers a TimeNTPServer that relays each call to conn, the
// dispensed connection of the module currently providing time.ntp/v1 (chrony).
func ForwardTimeNTP(s *grpc.Server, conn *grpc.ClientConn) {
	timentpv1.RegisterTimeNTPServer(s, &forwardingTimeNTP{client: timentpv1.NewTimeNTPClient(conn)})
}

type forwardingTimeNTP struct {
	timentpv1.UnimplementedTimeNTPServer
	client timentpv1.TimeNTPClient
}

func (f *forwardingTimeNTP) Endpoint(ctx context.Context, req *timentpv1.Empty) (*timentpv1.EndpointInfo, error) {
	return f.client.Endpoint(ctx, req)
}
