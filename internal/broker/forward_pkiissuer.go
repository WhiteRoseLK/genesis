// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	pkiissuerv1 "genesis/sdk/go/gen/functions/pki/issuer/v1"
)

// ForwardPkiIssuer enregistre un PkiIssuerServer qui relaie chaque appel
// vers conn, la connexion dispensée du module qui fournit pki.issuer/v1
// (step-ca en phase graine, vault en cible).
func ForwardPkiIssuer(s *grpc.Server, conn *grpc.ClientConn) {
	pkiissuerv1.RegisterPkiIssuerServer(s, &forwardingPkiIssuer{client: pkiissuerv1.NewPkiIssuerClient(conn)})
}

type forwardingPkiIssuer struct {
	pkiissuerv1.UnimplementedPkiIssuerServer
	client pkiissuerv1.PkiIssuerClient
}

func (f *forwardingPkiIssuer) IssueCert(ctx context.Context, req *pkiissuerv1.IssueCertRequest) (*pkiissuerv1.Certificate, error) {
	return f.client.IssueCert(ctx, req)
}

func (f *forwardingPkiIssuer) SignCSR(ctx context.Context, req *pkiissuerv1.SignCSRRequest) (*pkiissuerv1.Certificate, error) {
	return f.client.SignCSR(ctx, req)
}

func (f *forwardingPkiIssuer) SignSSH(ctx context.Context, req *pkiissuerv1.SignSSHRequest) (*pkiissuerv1.SSHCertificate, error) {
	return f.client.SignSSH(ctx, req)
}

func (f *forwardingPkiIssuer) CAChain(ctx context.Context, req *pkiissuerv1.Empty) (*pkiissuerv1.CAChainResponse, error) {
	return f.client.CAChain(ctx, req)
}
