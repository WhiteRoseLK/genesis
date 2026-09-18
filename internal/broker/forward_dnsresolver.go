// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	dnsresolverv1 "genesis/sdk/go/gen/functions/dns/resolver/v1"
)

// ForwardDNSResolver enregistre un DnsResolverServer qui relaie chaque appel
// vers conn, la connexion dispensée du module qui fournit dns.resolver/v1.
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
