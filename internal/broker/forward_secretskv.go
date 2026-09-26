// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	secretskvv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/secrets/kv/v1"
)

// ForwardSecretsKV enregistre un SecretsKVServer qui relaie chaque appel
// vers conn, la connexion dispensée du module qui fournit secrets.kv/v1
// (vault en cible).
func ForwardSecretsKV(s *grpc.Server, conn *grpc.ClientConn) {
	secretskvv1.RegisterSecretsKVServer(s, &forwardingSecretsKV{client: secretskvv1.NewSecretsKVClient(conn)})
}

type forwardingSecretsKV struct {
	secretskvv1.UnimplementedSecretsKVServer
	client secretskvv1.SecretsKVClient
}

func (f *forwardingSecretsKV) Read(ctx context.Context, req *secretskvv1.ReadRequest) (*secretskvv1.ReadResponse, error) {
	return f.client.Read(ctx, req)
}

func (f *forwardingSecretsKV) Write(ctx context.Context, req *secretskvv1.WriteRequest) (*secretskvv1.WriteResponse, error) {
	return f.client.Write(ctx, req)
}

func (f *forwardingSecretsKV) List(ctx context.Context, req *secretskvv1.ListRequest) (*secretskvv1.ListResponse, error) {
	return f.client.List(ctx, req)
}
