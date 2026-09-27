// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"
	"fmt"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/WhiteRoseLK/genesis/internal/secrets"
	secretsv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/secrets/v1"
)

// NativeSecrets builds the core.secrets/v1 provider, native to the core (never
// a module, docs/03-module-contract.md). The caller can only read (Get) a
// secret it owns or consumes (docs/02-architecture.md: "a module... can read
// only its own secrets and those explicitly shared").
func NativeSecrets(store secrets.Store) nativeFactory {
	return func(caller string) func(*grpc.Server) {
		return func(s *grpc.Server) {
			secretsv1.RegisterSecretsServer(s, &secretsServer{store: store, caller: caller})
		}
	}
}

type secretsServer struct {
	secretsv1.UnimplementedSecretsServer
	store  secrets.Store
	caller string
}

func (s *secretsServer) Ensure(ctx context.Context, req *secretsv1.EnsureRequest) (*secretsv1.EnsureResponse, error) {
	meta := fromProtoMeta(req.GetMeta())
	if meta.Owner == "" {
		meta.Owner = s.caller
	}
	if meta.Owner != s.caller {
		return nil, status.Errorf(codes.PermissionDenied, "module %q cannot create a secret on behalf of %q", s.caller, meta.Owner)
	}
	gen, err := generatorFunc(req.GetGenerator())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.store.Ensure(ctx, secrets.Ref(req.GetRef()), gen, meta); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &secretsv1.EnsureResponse{}, nil
}

func (s *secretsServer) Get(ctx context.Context, req *secretsv1.GetRequest) (*secretsv1.GetResponse, error) {
	ref := secrets.Ref(req.GetRef())
	meta, err := s.store.GetMeta(ctx, ref)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	if !s.authorized(meta) {
		return nil, status.Errorf(codes.PermissionDenied, "module %q is not allowed to read secret %q", s.caller, ref)
	}
	value, err := s.store.Get(ctx, ref)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &secretsv1.GetResponse{Value: value.ExposeSecret()}, nil
}

func (s *secretsServer) Put(ctx context.Context, req *secretsv1.PutRequest) (*secretsv1.PutResponse, error) {
	meta := fromProtoMeta(req.GetMeta())
	if meta.Owner == "" {
		meta.Owner = s.caller
	}
	if meta.Owner != s.caller {
		return nil, status.Errorf(codes.PermissionDenied, "module %q cannot write a secret on behalf of %q", s.caller, meta.Owner)
	}
	if err := s.store.Put(ctx, secrets.Ref(req.GetRef()), secrets.NewSecret(req.GetValue()), meta); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &secretsv1.PutResponse{}, nil
}

func (s *secretsServer) List(ctx context.Context, req *secretsv1.ListRequest) (*secretsv1.ListResponse, error) {
	entries, err := s.store.List(ctx, req.GetPrefix())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp := &secretsv1.ListResponse{}
	for _, e := range entries {
		if !s.authorized(e.Meta) {
			continue
		}
		resp.Entries = append(resp.Entries, &secretsv1.Entry{Ref: string(e.Ref), Meta: toProtoMeta(e.Meta)})
	}
	return resp, nil
}

func (s *secretsServer) authorized(meta secrets.Meta) bool {
	return meta.Owner == s.caller || slices.Contains(meta.Consumers, s.caller)
}

func generatorFunc(g secretsv1.Generator) (secrets.Generator, error) {
	switch g {
	case secretsv1.Generator_GENERATOR_PASSWORD:
		return secrets.GeneratePassword(), nil
	case secretsv1.Generator_GENERATOR_TOKEN:
		return secrets.GenerateToken(), nil
	case secretsv1.Generator_GENERATOR_ECDSA_P384:
		return secrets.GenerateECDSAP384Key(), nil
	case secretsv1.Generator_GENERATOR_ED25519:
		return secrets.GenerateEd25519Key(), nil
	case secretsv1.Generator_GENERATOR_SSH_KEYPAIR:
		return secrets.GenerateSSHKeyPair(), nil
	default:
		return nil, fmt.Errorf("unknown generator %v", g)
	}
}

func fromProtoMeta(m *secretsv1.Meta) secrets.Meta {
	if m == nil {
		return secrets.Meta{}
	}
	return secrets.Meta{
		Owner:     m.GetOwner(),
		Consumers: m.GetConsumers(),
		Kind:      m.GetKind(),
		Rotation:  m.GetRotation(),
		Recovery:  m.GetRecovery(),
	}
}

func toProtoMeta(m secrets.Meta) *secretsv1.Meta {
	return &secretsv1.Meta{
		Owner:     m.Owner,
		Consumers: m.Consumers,
		Kind:      m.Kind,
		Rotation:  m.Rotation,
		Recovery:  m.Recovery,
	}
}
