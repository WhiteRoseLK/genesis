// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	fleetagentv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/fleet/agent/v1"
)

// ForwardFleetAgent registers a FleetAgentServer that relays each call to ALL
// the provided connections (docs/09-decisions.md ADR-017) — unlike the "single
// active" forwarders (forward_*.go), which only ever relay to one connection.
func ForwardFleetAgent(s *grpc.Server, conns []*grpc.ClientConn) {
	fleetagentv1.RegisterFleetAgentServer(s, &fanoutFleetAgent{conns: conns})
}

type fanoutFleetAgent struct {
	fleetagentv1.UnimplementedFleetAgentServer
	conns []*grpc.ClientConn
}

func (f *fanoutFleetAgent) Install(ctx context.Context, req *fleetagentv1.InstallRequest) (*fleetagentv1.InstallResponse, error) {
	for i, conn := range f.conns {
		client := fleetagentv1.NewFleetAgentClient(conn)
		if _, err := client.Install(ctx, req); err != nil {
			return nil, fmt.Errorf("fleet.agent/v1.Install (provider %d/%d): %w", i+1, len(f.conns), err)
		}
	}
	return &fleetagentv1.InstallResponse{}, nil
}
