// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"strings"

	"google.golang.org/grpc"
)

// knownForwarders maps each "official" SDK function
// (docs/03-module-contract.md §3) provided by a module to its forwarder. It
// grows with the milestones that add real functions (compute.vm/v1 in M4;
// dns.zone/v1, pki.issuer/v1... followed with their modules, M5+).
var knownForwarders = map[string]func(*grpc.Server, *grpc.ClientConn){
	"compute.vm/v1":   ForwardComputeVM,
	"os.base/v1":      ForwardOSBase,
	"dns.zone/v1":     ForwardDNSZone,
	"dns.resolver/v1": ForwardDNSResolver,
	"time.ntp/v1":     ForwardTimeNTP,
	"pki.issuer/v1":   ForwardPkiIssuer,
	"secrets.kv/v1":   ForwardSecretsKV,
}

// ForwarderFor returns the forwarder to use for a function provided by a
// module. The generic test functions (test.*/v1, docs/08-milestones.md M4) all
// share the Echo service, with no dedicated entry in knownForwarders.
func ForwarderFor(function string) (func(*grpc.Server, *grpc.ClientConn), bool) {
	if f, ok := knownForwarders[function]; ok {
		return f, true
	}
	if strings.HasPrefix(function, "test.") {
		return ForwardEcho, true
	}
	return nil, false
}

// knownFleetForwarders maps each "fleet" function (ADR-017) to its fan-out
// forwarder (one connection per installed provider, all of them called).
var knownFleetForwarders = map[string]func(*grpc.Server, []*grpc.ClientConn){
	"fleet.agent/v1": ForwardFleetAgent,
}

// fleetForwarderFor returns the fan-out forwarder to use for a "fleet"
// function.
func fleetForwarderFor(function string) (func(*grpc.Server, []*grpc.ClientConn), bool) {
	f, ok := knownFleetForwarders[function]
	return f, ok
}
