// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"strconv"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// OpenSession démarre, sur le canal bidirectionnel go-plugin du module
// appelant (caller), une session de broker n'exposant que les fonctions
// listées dans allowed. Le jeton retourné est à placer dans
// StepRequest.broker_token avant d'invoquer l'étape correspondante
// (docs/03-contrat-module.md §2).
func (r *Registry) OpenSession(pb *goplugin.GRPCBroker, caller string, allowed []string) string {
	id := pb.NextId()
	pb.AcceptAndServe(id, func(opts []grpc.ServerOption) *grpc.Server {
		return r.BuildSession(caller, allowed, opts...)
	})
	return strconv.FormatUint(uint64(id), 10)
}
