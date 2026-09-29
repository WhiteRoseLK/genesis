// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"regexp"
	"time"

	accesssshv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/access/ssh/v1"
)

const (
	proxyPort          = 3080
	defaultUserCertTTL = time.Hour
)

// principalRe keeps the derived Teleport user name predictable and safe to
// interpolate into an Ansible command line (tctl users add): Teleport user
// names accept a broader charset, but nothing in this codebase needs more.
var principalRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// teleportAccessServer implements access.ssh/v1 (docs/03-module-contract.md).
type teleportAccessServer struct {
	accesssshv1.UnimplementedAccessSSHServer
	module *teleportModule
}

func (s *teleportAccessServer) JumpHost(context.Context, *accesssshv1.Empty) (*accesssshv1.JumpHostInfo, error) {
	m := s.module
	m.mu.Lock()
	host := m.ownTarget.Host
	m.mu.Unlock()
	if host == "" {
		return nil, fmt.Errorf("access.ssh/v1: teleport not configured yet (Configure has not succeeded yet)")
	}
	return &accesssshv1.JumpHostInfo{Address: host, Port: proxyPort}, nil
}

// SignUserKey signs a short-lived SSH user certificate through Teleport's own
// internal CA (tctl auth sign --format=openssh, docs/adr/0018). Teleport
// cannot certify an externally supplied public key (verified against a real
// Teleport container: tctl auth sign only ever generates its own keypair) --
// request.public_key_openssh is therefore ignored, and the generated private
// key travels back in response.private_key_openssh (ssh.proto).
func (s *teleportAccessServer) SignUserKey(ctx context.Context, req *accesssshv1.SignUserKeyRequest) (*accesssshv1.SignUserKeyResponse, error) {
	m := s.module
	m.mu.Lock()
	ownTarget := m.ownTarget
	m.mu.Unlock()
	if ownTarget.Host == "" {
		return nil, fmt.Errorf("access.ssh/v1.SignUserKey: teleport not configured yet (Configure has not succeeded yet)")
	}

	principals := req.GetPrincipals()
	if len(principals) == 0 {
		return nil, fmt.Errorf("access.ssh/v1.SignUserKey: at least one principal required")
	}
	for _, p := range principals {
		if !principalRe.MatchString(p) {
			return nil, fmt.Errorf("access.ssh/v1.SignUserKey: invalid principal %q", p)
		}
	}

	ttl := defaultUserCertTTL
	if ttlSeconds := req.GetTtlSeconds(); ttlSeconds > 0 {
		ttl = time.Duration(ttlSeconds) * time.Second
	}

	teleportUser := "genesis-" + principals[0]
	privPEM, certOpenSSH, err := m.signUserCert(ctx, ownTarget, teleportUser, principals, ttl)
	if err != nil {
		return nil, fmt.Errorf("access.ssh/v1.SignUserKey: %w", err)
	}
	return &accesssshv1.SignUserKeyResponse{CertificateOpenssh: certOpenSSH, PrivateKeyOpenssh: privPEM}, nil
}
