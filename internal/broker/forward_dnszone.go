// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
)

// ForwardDNSZone registers a DnsZoneServer that relays each call to conn, the
// dispensed connection of the module currently providing dns.zone/v1 (coredns
// in the seed phase, powerdns as the target).
func ForwardDNSZone(s *grpc.Server, conn *grpc.ClientConn) {
	dnszonev1.RegisterDnsZoneServer(s, &forwardingDNSZone{client: dnszonev1.NewDnsZoneClient(conn)})
}

type forwardingDNSZone struct {
	dnszonev1.UnimplementedDnsZoneServer
	client dnszonev1.DnsZoneClient
}

func (f *forwardingDNSZone) UpsertRecord(ctx context.Context, req *dnszonev1.Record) (*dnszonev1.Empty, error) {
	return f.client.UpsertRecord(ctx, req)
}

func (f *forwardingDNSZone) DeleteRecord(ctx context.Context, req *dnszonev1.RecordKey) (*dnszonev1.Empty, error) {
	return f.client.DeleteRecord(ctx, req)
}

func (f *forwardingDNSZone) ListRecords(ctx context.Context, req *dnszonev1.Zone) (*dnszonev1.Records, error) {
	return f.client.ListRecords(ctx, req)
}

func (f *forwardingDNSZone) Endpoint(ctx context.Context, req *dnszonev1.Empty) (*dnszonev1.EndpointInfo, error) {
	return f.client.Endpoint(ctx, req)
}
