// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"strings"

	"google.golang.org/grpc"
)

// knownForwarders associe chaque fonction "officielle" du SDK (docs/03-contrat-module.md
// §3) fournie par un module à son forwarder. Grandit avec les jalons qui
// ajoutent de vraies fonctions (compute.vm/v1 en J4 ; dns.zone/v1,
// pki.issuer/v1... suivront avec leurs modules, J5+).
var knownForwarders = map[string]func(*grpc.Server, *grpc.ClientConn){
	"compute.vm/v1":   ForwardComputeVM,
	"os.base/v1":      ForwardOSBase,
	"dns.zone/v1":     ForwardDNSZone,
	"dns.resolver/v1": ForwardDNSResolver,
	"time.ntp/v1":     ForwardTimeNTP,
	"pki.issuer/v1":   ForwardPkiIssuer,
	"secrets.kv/v1":   ForwardSecretsKV,
}

// ForwarderFor retourne le forwarder à utiliser pour function fournie par un
// module. Les fonctions de test génériques (test.*/v1, docs/08-jalons.md J4)
// partagent toutes le service Echo, sans entrée dédiée dans knownForwarders.
func ForwarderFor(function string) (func(*grpc.Server, *grpc.ClientConn), bool) {
	if f, ok := knownForwarders[function]; ok {
		return f, true
	}
	if strings.HasPrefix(function, "test.") {
		return ForwardEcho, true
	}
	return nil, false
}

// knownFleetForwarders associe chaque fonction « de parc » (ADR-017) à son
// forwarder fan-out (une connexion par fournisseur installé, tous appelés).
var knownFleetForwarders = map[string]func(*grpc.Server, []*grpc.ClientConn){
	"fleet.agent/v1": ForwardFleetAgent,
}

// fleetForwarderFor retourne le forwarder fan-out à utiliser pour une
// fonction « de parc ».
func fleetForwarderFor(function string) (func(*grpc.Server, []*grpc.ClientConn), bool) {
	f, ok := knownFleetForwarders[function]
	return f, ok
}
