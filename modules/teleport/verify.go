// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	sdk "genesis/sdk/go"
	computevmv1 "genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "genesis/sdk/go/gen/functions/core/ansible/v1"
	fleetagentv1 "genesis/sdk/go/gen/functions/fleet/agent/v1"
	modulev1 "genesis/sdk/go/gen/module/v1"
)

var (
	privateKeyB64Re = regexp.MustCompile(`PRIVATE_KEY_B64:(\S+)`)
	certificateB64  = regexp.MustCompile(`CERTIFICATE_B64:(\S+)`)
)

// Verify prouve, depuis une VM tierce jetable, une connexion SSH réelle à
// travers l'agent Teleport installé sur une deuxième VM jetable
// (docs/03-contrat-module.md règle 2 : point de vue consommateur). Le sshd
// natif de la cible n'est pas désactivé à ce jalon (Repoint différé,
// main.go) : Verify ne teste donc que la connexion via l'agent, pas le
// refus de l'accès direct.
func (m *teleportModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	pair, err := m.sshKeyPair(ctx, name)
	if err != nil {
		return nil, err
	}
	ownTarget := targetFromState(req, pair)

	agentTargetName := name + agentTargetSuffix
	agentPair, err := m.sshKeyPair(ctx, agentTargetName)
	if err != nil {
		return nil, err
	}
	agentVM, err := m.vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name: agentTargetName, Env: agentTargetName, SshPublicKey: agentPair.PublicKeyAuthorized, User: sshUser,
	})
	if err != nil {
		return nil, fmt.Errorf("EnsureVM(%q) (cible de l'agent) : %w", agentTargetName, err)
	}
	defer func() {
		_, _ = m.vmClient.DeleteVM(context.Background(), &computevmv1.DeleteVMRequest{Name: agentTargetName})
	}()

	if err := m.installAgent(ctx, ownTarget, &fleetagentv1.Target{
		Host: agentVM.GetIp(), Port: agentVM.GetSshPort(), User: sshUser, SshPrivateKey: agentPair.PrivateKeyOpenSSH,
	}); err != nil {
		return nil, fmt.Errorf("Verify(teleport) : installation de l'agent sur la cible de test : %w", err)
	}

	privKeyPEM, certOpenSSH, err := m.generateUserCert(ctx, ownTarget)
	if err != nil {
		return nil, fmt.Errorf("Verify(teleport) : %w", err)
	}

	verifierName := name + verifierSuffix
	verifierPair, err := m.sshKeyPair(ctx, verifierName)
	if err != nil {
		return nil, err
	}
	verifierVM, err := m.vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name: verifierName, Env: verifierName, SshPublicKey: verifierPair.PublicKeyAuthorized, User: sshUser,
	})
	if err != nil {
		return nil, fmt.Errorf("EnsureVM(%q) (vérificateur) : %w", verifierName, err)
	}
	defer func() {
		_, _ = m.vmClient.DeleteVM(context.Background(), &computevmv1.DeleteVMRequest{Name: verifierName})
	}()

	verifierTarget := &ansiblev1.Target{
		Host: verifierVM.GetIp(), Port: verifierVM.GetSshPort(), User: sshUser, SshPrivateKey: verifierPair.PrivateKeyOpenSSH,
	}
	vars, err := sdk.NewState(map[string]any{
		"user_private_key_pem":     privKeyPEM,
		"user_certificate_openssh": certOpenSSH,
		"agent_host":               agentVM.GetIp(),
		"agent_user":               sshUser,
	})
	if err != nil {
		return nil, err
	}
	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target: verifierTarget, PlaybookYaml: checkSSHPlaybook, Vars: vars,
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetOk() {
		return nil, fmt.Errorf("Verify(teleport) a échoué :\n%s", resp.GetOutput())
	}
	if !strings.Contains(resp.GetOutput(), "AGENT_SSH_OK") {
		return nil, fmt.Errorf("Verify(teleport) : sortie inattendue, attendu AGENT_SSH_OK :\n%s", resp.GetOutput())
	}

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

// generateUserCert crée (idempotent) un utilisateur Teleport de
// vérification et signe un certificat SSH court terme pour lui (tctl auth
// sign --format=openssh), sur la propre VM teleport.
func (m *teleportModule) generateUserCert(ctx context.Context, ownTarget connTarget) (privKeyPEM, certOpenSSH string, err error) {
	vars, err := sdk.NewState(map[string]any{
		"verify_user": verifyTeleportUser,
		"ssh_login":   sshUser,
		"cert_ttl":    verifyCertTTL,
	})
	if err != nil {
		return "", "", err
	}
	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target: ownTarget.ansible(), PlaybookYaml: generateUserCertPlaybook, Vars: vars,
	})
	if err != nil {
		return "", "", err
	}
	if !resp.GetOk() {
		return "", "", fmt.Errorf("génération du certificat utilisateur de vérification a échoué :\n%s", resp.GetOutput())
	}
	privMatch := privateKeyB64Re.FindStringSubmatch(resp.GetOutput())
	certMatch := certificateB64.FindStringSubmatch(resp.GetOutput())
	if privMatch == nil || certMatch == nil {
		return "", "", fmt.Errorf("clé privée/certificat introuvables dans la sortie de tctl auth sign :\n%s", resp.GetOutput())
	}
	priv, err := base64.StdEncoding.DecodeString(privMatch[1])
	if err != nil {
		return "", "", fmt.Errorf("décodage de la clé privée générée : %w", err)
	}
	cert, err := base64.StdEncoding.DecodeString(certMatch[1])
	if err != nil {
		return "", "", fmt.Errorf("décodage du certificat généré : %w", err)
	}
	return string(priv), string(cert), nil
}
