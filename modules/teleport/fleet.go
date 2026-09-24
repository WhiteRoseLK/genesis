// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"regexp"

	sdk "genesis/sdk/go"
	ansiblev1 "genesis/sdk/go/gen/functions/core/ansible/v1"
	fleetagentv1 "genesis/sdk/go/gen/functions/fleet/agent/v1"
)

var joinTokenRe = regexp.MustCompile(`[a-f0-9]{32}`)

// teleportFleetServer implémente fleet.agent/v1 (ADR-017) : Install génère
// un jeton d'enrôlement à usage court (tctl tokens add, sur la propre VM de
// teleport) puis installe/enrôle l'agent SSH sur la VM cible -- le sshd
// natif de la cible n'est volontairement PAS désactivé à ce jalon (Repoint
// différé, voir le commentaire de package dans main.go).
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
		return nil, fmt.Errorf("fleet.agent/v1.Install : teleport pas encore configuré (Configure n'a pas encore réussi)")
	}

	t := req.GetTarget()
	if t == nil || t.GetHost() == "" {
		return nil, fmt.Errorf("fleet.agent/v1.Install : target manquant")
	}

	if err := m.installAgent(ctx, ownTarget, t); err != nil {
		return nil, fmt.Errorf("fleet.agent/v1.Install(%s) : %w", t.GetHost(), err)
	}
	return &fleetagentv1.InstallResponse{}, nil
}

// installAgent génère un jeton d'enrôlement puis déploie/enrôle l'agent
// Teleport sur la cible -- factorisé pour être appelé à la fois par
// Install (consommateurs réels) et par Verify (VM jetable de test).
func (m *teleportModule) installAgent(ctx context.Context, ownTarget connTarget, t *fleetagentv1.Target) error {
	m.mu.Lock()
	caPin := m.caPin
	m.mu.Unlock()

	joinToken, err := m.newJoinToken(ctx, ownTarget)
	if err != nil {
		return fmt.Errorf("génération du jeton d'enrôlement : %w", err)
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
		return fmt.Errorf("échec :\n%s", resp.GetOutput())
	}
	return nil
}

// newJoinToken génère un jeton d'enrôlement Teleport à usage court (tctl
// tokens add --type=node) sur la propre VM de teleport -- un jeton par
// enrôlement, jamais réutilisé (docs/09-decisions.md ADR-017).
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
		return "", fmt.Errorf("échec :\n%s", resp.GetOutput())
	}
	token := joinTokenRe.FindString(resp.GetOutput())
	if token == "" {
		return "", fmt.Errorf("jeton introuvable dans la sortie de tctl tokens add :\n%s", resp.GetOutput())
	}
	return token, nil
}
