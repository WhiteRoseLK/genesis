// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"regexp"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	dnsresolverv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/resolver/v1"
	osbasev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	pkiissuerv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/pki/issuer/v1"
	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

var caPinRe = regexp.MustCompile(`sha256:[0-9a-f]{64}`)

// Configure installe Teleport (Auth + Proxy) sur sa propre VM, avec un
// certificat TLS émis par pki.issuer/v1 (résolu vers vault, déjà actif à ce
// stade du DAG, docs/07-mvp-modules.md), puis calcule le pin de la CA
// interne Teleport (tctl status) réutilisé par fleet.agent/v1.Install pour
// chaque enrôlement (docs/09-decisions.md ADR-017/018).
func (m *teleportModule) Configure(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	pair, err := m.sshKeyPair(ctx, name)
	if err != nil {
		return nil, err
	}
	target := targetFromState(req, pair)
	clusterName := clusterNameFromConfig(req)

	ntpEndpoint, err := m.timeNTPClient.Endpoint(ctx, &timentpv1.Empty{})
	if err != nil {
		return nil, fmt.Errorf("lecture de time.ntp/v1 : %w", err)
	}
	if _, err := m.osBaseClient.SetNTP(ctx, &osbasev1.SetNTPRequest{Target: target.osBase(), Servers: []string{ntpEndpoint.GetAddress()}}); err != nil {
		return nil, fmt.Errorf("SetNTP : %w", err)
	}
	resolverEndpoint, err := m.dnsResolverClient.Endpoint(ctx, &dnsresolverv1.Empty{})
	if err != nil {
		return nil, fmt.Errorf("lecture de dns.resolver/v1 : %w", err)
	}
	if _, err := m.osBaseClient.SetResolver(ctx, &osbasev1.SetResolverRequest{Target: target.osBase(), Nameservers: []string{resolverEndpoint.GetAddress()}}); err != nil {
		return nil, fmt.Errorf("SetResolver : %w", err)
	}

	tlsCert, err := m.pkiClient.IssueCert(ctx, &pkiissuerv1.IssueCertRequest{
		CommonName: target.Host, Sans: []string{target.Host}, TtlSeconds: leafTTLSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("émission du certificat TLS de teleport via pki.issuer/v1 : %w", err)
	}
	caChain, err := m.pkiClient.CAChain(ctx, &pkiissuerv1.Empty{})
	if err != nil {
		return nil, fmt.Errorf("lecture de pki.issuer/v1.CAChain : %w", err)
	}

	output, err := m.deployTeleport(ctx, target, clusterName, tlsCert, caChain.GetChainPem())
	if err != nil {
		return nil, err
	}

	caPin := caPinRe.FindString(output)
	if caPin == "" {
		return nil, fmt.Errorf("pin de la CA introuvable dans la sortie de tctl status :\n%s", output)
	}

	m.mu.Lock()
	m.ownTarget = target
	m.clusterName = clusterName
	m.caPin = caPin
	m.mu.Unlock()

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

// deployTeleport installe/configure Teleport via ansible et dépose le
// certificat TLS fourni -- rejoué tel quel sans risque (playbook idempotent,
// ne redémarre que si le TLS ou la config a changé).
func (m *teleportModule) deployTeleport(ctx context.Context, target connTarget, clusterName string, cert *pkiissuerv1.Certificate, caChainPEM string) (string, error) {
	vars, err := sdk.NewState(map[string]any{
		"node_name":    target.Host,
		"cluster_name": clusterName,
		"vm_ip":        target.Host,
		"tls_cert_pem": cert.GetChainPem(),
		"tls_key_pem":  cert.GetPrivateKeyPem(),
		"ca_chain_pem": caChainPEM,
	})
	if err != nil {
		return "", err
	}
	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target: target.ansible(), PlaybookYaml: installTeleportPlaybook, Vars: vars,
	})
	if err != nil {
		return "", err
	}
	if !resp.GetOk() {
		return "", fmt.Errorf("déploiement de teleport a échoué :\n%s", resp.GetOutput())
	}
	return resp.GetOutput(), nil
}
