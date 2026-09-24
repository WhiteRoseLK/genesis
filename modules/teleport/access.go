// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accesssshv1 "genesis/sdk/go/gen/functions/access/ssh/v1"
)

const proxyPort = 3080

// teleportAccessServer implémente access.ssh/v1 (docs/03-contrat-module.md).
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
		return nil, fmt.Errorf("access.ssh/v1 : teleport pas encore configuré (Configure n'a pas encore réussi)")
	}
	return &accesssshv1.JumpHostInfo{Address: host, Port: proxyPort}, nil
}

// SignUserKey : différé, pas encore de consommateur réel à ce jalon
// (docs/PROGRESS.md, même méthode que modules/vault.SignSSH et
// modules/step-ca.SignSSH) -- signerait une clé publique utilisateur
// fournie en certificat SSH court terme via tctl auth sign.
func (s *teleportAccessServer) SignUserKey(context.Context, *accesssshv1.SignUserKeyRequest) (*accesssshv1.SignUserKeyResponse, error) {
	return nil, status.Error(codes.Unimplemented, "SignUserKey : pas encore de consommateur réel à ce jalon (docs/PROGRESS.md, même méthode que modules/vault.SignSSH)")
}
