// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"regexp"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	fleetagentv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/fleet/agent/v1"
)

var joinTokenRe = regexp.MustCompile(`[a-f0-9]{32}`)

// teleportFleetServer implements fleet.agent/v1 (ADR-017): Install generates a
// short-lived enrolment token (tctl tokens add, on teleport's own VM) then
// installs/enrols the SSH agent on the target VM -- the target's native sshd
// is deliberately NOT disabled at this milestone (deferred Repoint, see the
// package comment in main.go).
type teleportFleetServer struct {
	fleetagentv1.UnimplementedFleetAgentServer
	module *teleportModule
}

func (s *teleportFleetServer) Install(ctx context.Context, req *fleetagentv1.InstallRequest) (*fleetagentv1.InstallResponse, error) {
	m := s.module
	m.mu.Lock()
	ownTarget := m.ownTarget
	m.mu.Unlock()
	if ownTarget.Host == "" {
		return nil, fmt.Errorf("fleet.agent/v1.Install: teleport not configured yet (Configure has not succeeded yet)")
	}

	t := req.GetTarget()
	if t == nil || t.GetHost() == "" {
		return nil, fmt.Errorf("fleet.agent/v1.Install: missing target")
	}

	if err := m.installAgent(ctx, ownTarget, t); err != nil {
		return nil, fmt.Errorf("fleet.agent/v1.Install(%s): %w", t.GetHost(), err)
	}
	return &fleetagentv1.InstallResponse{}, nil
}

// installAgent generates an enrolment token then deploys/enrols the Teleport
// agent on the target -- factored out to be called both by Install (real
// consumers) and by Verify (disposable test VM).
func (m *teleportModule) installAgent(ctx context.Context, ownTarget connTarget, t *fleetagentv1.Target) error {
	m.mu.Lock()
	caPin := m.caPin
	m.mu.Unlock()

	joinToken, err := m.newJoinToken(ctx, ownTarget)
	if err != nil {
		return fmt.Errorf("generating the enrolment token: %w", err)
	}

	agentTarget := &ansiblev1.Target{Host: t.GetHost(), Port: t.GetPort(), User: t.GetUser(), SshPrivateKey: t.GetSshPrivateKey()}
	vars, err := sdk.NewState(map[string]any{
		"node_name":   t.GetHost(),
		"join_token":  joinToken,
		"ca_pin":      caPin,
		"auth_server": fmt.Sprintf("%s:%d", ownTarget.Host, authPort),
	})
	if err != nil {
		return err
	}
	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target: agentTarget, PlaybookYaml: installAgentPlaybook, Vars: vars,
	})
	if err != nil {
		return err
	}
	if !resp.GetOk() {
		return fmt.Errorf("failed:\n%s", resp.GetOutput())
	}
	return nil
}

// newJoinToken generates a short-lived Teleport enrolment token (tctl tokens
// add --type=node) on teleport's own VM -- one token per enrolment, never
// reused (docs/09-decisions.md ADR-017).
func (m *teleportModule) newJoinToken(ctx context.Context, ownTarget connTarget) (string, error) {
	vars, err := sdk.NewState(map[string]any{"join_ttl": joinTokenTTL})
	if err != nil {
		return "", err
	}
	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target: ownTarget.ansible(), PlaybookYaml: newJoinTokenPlaybook, Vars: vars,
	})
	if err != nil {
		return "", err
	}
	if !resp.GetOk() {
		return "", fmt.Errorf("failed:\n%s", resp.GetOutput())
	}
	token := joinTokenRe.FindString(resp.GetOutput())
	if token == "" {
		return "", fmt.Errorf("token not found in the output of tctl tokens add:\n%s", resp.GetOutput())
	}
	return token, nil
}
