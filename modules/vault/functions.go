// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pkiissuerv1 "genesis/sdk/go/gen/functions/pki/issuer/v1"
	secretskvv1 "genesis/sdk/go/gen/functions/secrets/kv/v1"
)

func (m *vaultModule) ready() (*vaultClient, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.api == nil || m.approleToken == "" {
		return nil, "", fmt.Errorf("pki.issuer/v1 : vault pas encore configuré (Configure n'a pas encore réussi)")
	}
	return m.api, m.approleToken, nil
}

func (m *vaultModule) IssueCert(ctx context.Context, req *pkiissuerv1.IssueCertRequest) (*pkiissuerv1.Certificate, error) {
	api, token, err := m.ready()
	if err != nil {
		return nil, err
	}
	ttl := ""
	if req.GetTtlSeconds() > 0 {
		ttl = fmt.Sprintf("%ds", req.GetTtlSeconds())
	}
	issued, err := api.issueCert(ctx, token, pkiMount, pkiRole, req.GetCommonName(), req.GetSans(), ttl)
	if err != nil {
		return nil, fmt.Errorf("IssueCert(%q) : %w", req.GetCommonName(), err)
	}
	return &pkiissuerv1.Certificate{CertPem: issued.CertPEM, ChainPem: issued.ChainPEM, PrivateKeyPem: issued.PrivateKeyPEM}, nil
}

func (m *vaultModule) SignCSR(ctx context.Context, req *pkiissuerv1.SignCSRRequest) (*pkiissuerv1.Certificate, error) {
	api, token, err := m.ready()
	if err != nil {
		return nil, err
	}
	ttl := ""
	if req.GetTtlSeconds() > 0 {
		ttl = fmt.Sprintf("%ds", req.GetTtlSeconds())
	}
	signed, err := api.signCSR(ctx, token, pkiMount, pkiRole, req.GetCsrPem(), "", ttl)
	if err != nil {
		return nil, fmt.Errorf("SignCSR : %w", err)
	}
	return &pkiissuerv1.Certificate{CertPem: signed.CertPEM, ChainPem: signed.ChainPEM}, nil
}

func (m *vaultModule) SignSSH(context.Context, *pkiissuerv1.SignSSHRequest) (*pkiissuerv1.SSHCertificate, error) {
	return nil, status.Error(codes.Unimplemented, "SignSSH : différé à openssh-bastion (J8), pas encore de consommateur réel à ce jalon (docs/PROGRESS.md, même méthode que modules/step-ca)")
}

func (m *vaultModule) CAChain(context.Context, *pkiissuerv1.Empty) (*pkiissuerv1.CAChainResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pkiIntPEM == "" {
		return nil, fmt.Errorf("CAChain : vault pas encore configuré (Configure n'a pas encore réussi)")
	}
	return &pkiissuerv1.CAChainResponse{ChainPem: m.pkiIntPEM + m.rootCAPEM}, nil
}

// vaultPkiServer adapte pki.issuer/v1 vers les méthodes de vaultModule
// (même schéma que modules/coredns : le module porte la logique, le
// serveur grpc n'est qu'un adaptateur mince).
type vaultPkiServer struct {
	pkiissuerv1.UnimplementedPkiIssuerServer
	module *vaultModule
}

func (s *vaultPkiServer) IssueCert(ctx context.Context, req *pkiissuerv1.IssueCertRequest) (*pkiissuerv1.Certificate, error) {
	return s.module.IssueCert(ctx, req)
}

func (s *vaultPkiServer) SignCSR(ctx context.Context, req *pkiissuerv1.SignCSRRequest) (*pkiissuerv1.Certificate, error) {
	return s.module.SignCSR(ctx, req)
}

func (s *vaultPkiServer) SignSSH(ctx context.Context, req *pkiissuerv1.SignSSHRequest) (*pkiissuerv1.SSHCertificate, error) {
	return s.module.SignSSH(ctx, req)
}

func (s *vaultPkiServer) CAChain(ctx context.Context, req *pkiissuerv1.Empty) (*pkiissuerv1.CAChainResponse, error) {
	return s.module.CAChain(ctx, req)
}

// vaultKVServer implémente secrets.kv/v1 — utilisé par le cœur (migration
// file->vault, internal/engine) via le token AppRole, jamais le root token
// (docs/07-modules-mvp.md).
type vaultKVServer struct {
	secretskvv1.UnimplementedSecretsKVServer
	module *vaultModule
}

func (s *vaultKVServer) Read(ctx context.Context, req *secretskvv1.ReadRequest) (*secretskvv1.ReadResponse, error) {
	api, token, err := s.module.ready()
	if err != nil {
		return nil, err
	}
	value, found, err := api.kvRead(ctx, token, kvMount, req.GetRef())
	if err != nil {
		return nil, fmt.Errorf("Read(%q) : %w", req.GetRef(), err)
	}
	return &secretskvv1.ReadResponse{Value: value, Found: found}, nil
}

func (s *vaultKVServer) Write(ctx context.Context, req *secretskvv1.WriteRequest) (*secretskvv1.WriteResponse, error) {
	api, token, err := s.module.ready()
	if err != nil {
		return nil, err
	}
	if err := api.kvWrite(ctx, token, kvMount, req.GetRef(), req.GetValue()); err != nil {
		return nil, fmt.Errorf("Write(%q) : %w", req.GetRef(), err)
	}
	return &secretskvv1.WriteResponse{}, nil
}

func (s *vaultKVServer) List(context.Context, *secretskvv1.ListRequest) (*secretskvv1.ListResponse, error) {
	// LIST v2 KV nécessite la méthode HTTP LIST (non standard) ; pas encore
	// consommé par le cœur (la migration écrit/relit par ref connue, ne
	// liste jamais le contenu de vault) — stub explicite plutôt qu'une
	// implémentation non vérifiable (docs/PROGRESS.md, même méthode que
	// SignSSH).
	return nil, status.Error(codes.Unimplemented, "List : pas encore de consommateur réel à ce jalon (docs/PROGRESS.md)")
}
