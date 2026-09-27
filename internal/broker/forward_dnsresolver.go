// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	dnsresolverv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/resolver/v1"
)

// ForwardDNSResolver registers a DnsResolverServer that relays each call to
// conn, the dispensed connection of the module currently providing
// dns.resolver/v1.
func ForwardDNSResolver(s *grpc.Server, conn *grpc.ClientConn) {
	dnsresolverv1.RegisterDnsResolverServer(s, &forwardingDNSResolver{client: dnsresolverv1.NewDnsResolverClient(conn)})
}

type forwardingDNSResolver struct {
	dnsresolverv1.UnimplementedDnsResolverServer
	client dnsresolverv1.DnsResolverClient
}

func (f *forwardingDNSResolver) Endpoint(ctx context.Context, req *dnsresolverv1.Empty) (*dnsresolverv1.EndpointInfo, error) {
	return f.client.Endpoint(ctx, req)
}
