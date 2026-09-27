// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accesssshv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/access/ssh/v1"
)

const proxyPort = 3080

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

// SignUserKey: deferred, no real consumer yet at this milestone
// (docs/PROGRESS.md, the same method as modules/vault.SignSSH and
// modules/step-ca.SignSSH) -- it would sign a provided user public key into a
// short-lived SSH certificate through tctl auth sign.
func (s *teleportAccessServer) SignUserKey(context.Context, *accesssshv1.SignUserKeyRequest) (*accesssshv1.SignUserKeyResponse, error) {
	return nil, status.Error(codes.Unimplemented, "SignUserKey: no real consumer yet (docs/PROGRESS.md, the same method as modules/vault.SignSSH)")
}
