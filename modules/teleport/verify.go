// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"time"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	fleetagentv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/fleet/agent/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

var (
	privateKeyB64Re = regexp.MustCompile(`PRIVATE_KEY_B64:(\S+)`)
	certificateB64  = regexp.MustCompile(`CERTIFICATE_B64:(\S+)`)
)

// Verify proves, from a disposable third-party VM, a real SSH connection
// through the Teleport agent installed on a second disposable VM
// (docs/03-module-contract.md rule 2: a consumer's point of view). The
// target's native sshd is not disabled at this milestone (deferred Repoint,
// main.go): Verify therefore only tests the connection through the agent, not
// that direct access is refused.
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
		return nil, fmt.Errorf("EnsureVM(%q) (agent target): %w", agentTargetName, err)
	}
	defer func() {
		_, _ = m.vmClient.DeleteVM(context.Background(), &computevmv1.DeleteVMRequest{Name: agentTargetName})
	}()

	if err := m.installAgent(ctx, ownTarget, &fleetagentv1.Target{
		Host: agentVM.GetIp(), Port: agentVM.GetSshPort(), User: sshUser, SshPrivateKey: agentPair.PrivateKeyOpenSSH,
	}); err != nil {
		return nil, fmt.Errorf("Verify(teleport): installing the agent on the test target: %w", err)
	}

	privKeyPEM, certOpenSSH, err := m.signUserCert(ctx, ownTarget, verifyTeleportUser, []string{sshUser}, verifyCertTTL)
	if err != nil {
		return nil, fmt.Errorf("Verify(teleport): %w", err)
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
		return nil, fmt.Errorf("EnsureVM(%q) (verifier): %w", verifierName, err)
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
		return nil, fmt.Errorf("Verify(teleport) failed:\n%s", resp.GetOutput())
	}
	if !strings.Contains(resp.GetOutput(), "AGENT_SSH_OK") {
		return nil, fmt.Errorf("Verify(teleport): unexpected output, expected AGENT_SSH_OK:\n%s", resp.GetOutput())
	}

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

// signUserCert creates (idempotently) a Teleport user and signs a short-lived
// SSH certificate for it (tctl auth sign --format=openssh), on teleport's own
// VM. Shared by Verify (fixed verification user) and access.ssh/v1.SignUserKey
// (request-derived user/logins/ttl).
func (m *teleportModule) signUserCert(ctx context.Context, ownTarget connTarget, teleportUser string, logins []string, ttl time.Duration) (privKeyPEM, certOpenSSH string, err error) {
	vars, err := sdk.NewState(map[string]any{
		"verify_user": teleportUser,
		"ssh_login":   strings.Join(logins, ","),
		"cert_ttl":    ttl.String(),
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
		return "", "", fmt.Errorf("generating the verification user certificate failed:\n%s", resp.GetOutput())
	}
	privMatch := privateKeyB64Re.FindStringSubmatch(resp.GetOutput())
	certMatch := certificateB64.FindStringSubmatch(resp.GetOutput())
	if privMatch == nil || certMatch == nil {
		return "", "", fmt.Errorf("private key/certificate not found in the output of tctl auth sign:\n%s", resp.GetOutput())
	}
	priv, err := base64.StdEncoding.DecodeString(privMatch[1])
	if err != nil {
		return "", "", fmt.Errorf("decoding the generated private key: %w", err)
	}
	cert, err := base64.StdEncoding.DecodeString(certMatch[1])
	if err != nil {
		return "", "", fmt.Errorf("decoding the generated certificate: %w", err)
	}
	return string(priv), string(cert), nil
}
