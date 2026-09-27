// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"strconv"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// OpenSession starts, on the bidirectional go-plugin channel of the calling
// module (caller), a broker session that only exposes the functions listed in
// allowed. The returned token goes into StepRequest.broker_token before
// invoking the matching step (docs/03-module-contract.md §2).
//
// AcceptAndServe blocks until the session closes (it is an Accept+Serve, like
// http.Server.Serve): it therefore runs in its own goroutine. Nothing to wait
// for before returning the token — Dial, on the module side, itself waits up
// to 5 s for the connection info to arrive (go-plugin grpc_broker.go), no
// synchronisation needed here.
func (r *Registry) OpenSession(pb *goplugin.GRPCBroker, caller string, allowed []string) string {
	id := pb.NextId()
	go pb.AcceptAndServe(id, func(opts []grpc.ServerOption) *grpc.Server {
		return r.BuildSession(caller, allowed, opts...)
	})
	return strconv.FormatUint(uint64(id), 10)
}
