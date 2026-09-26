// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
)

// ForwardTimeNTP enregistre un TimeNTPServer qui relaie chaque appel vers
// conn, la connexion dispensée du module qui fournit time.ntp/v1 (chrony).
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
