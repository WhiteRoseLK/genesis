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

// Configure installs Teleport (Auth + Proxy) on its own VM, with a TLS
// certificate issued by pki.issuer/v1 (resolved to vault, already active at
// this point of the DAG, docs/07-mvp-modules.md), then computes the pin of
// Teleport's internal CA (tctl status), reused by fleet.agent/v1.Install for
// each enrolment (docs/09-decisions.md ADR-017/018).
func (m *teleportModule) Configure(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetBrokerToken()); err != nil {
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
		return nil, fmt.Errorf("reading time.ntp/v1: %w", err)
	}
	if _, err := m.osBaseClient.SetNTP(ctx, &osbasev1.SetNTPRequest{Target: target.osBase(), Servers: []string{ntpEndpoint.GetAddress()}}); err != nil {
		return nil, fmt.Errorf("SetNTP: %w", err)
	}
	resolverEndpoint, err := m.dnsResolverClient.Endpoint(ctx, &dnsresolverv1.Empty{})
	if err != nil {
		return nil, fmt.Errorf("reading dns.resolver/v1: %w", err)
	}
	if _, err := m.osBaseClient.SetResolver(ctx, &osbasev1.SetResolverRequest{Target: target.osBase(), Nameservers: []string{resolverEndpoint.GetAddress()}}); err != nil {
		return nil, fmt.Errorf("SetResolver: %w", err)
	}

	tlsCert, err := m.pkiClient.IssueCert(ctx, &pkiissuerv1.IssueCertRequest{
		CommonName: target.Host, Sans: []string{target.Host}, TtlSeconds: leafTTLSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("issuing teleport's TLS certificate through pki.issuer/v1: %w", err)
	}
	caChain, err := m.pkiClient.CAChain(ctx, &pkiissuerv1.Empty{})
	if err != nil {
		return nil, fmt.Errorf("reading pki.issuer/v1.CAChain: %w", err)
	}

	output, err := m.deployTeleport(ctx, target, clusterName, tlsCert, caChain.GetChainPem())
	if err != nil {
		return nil, err
	}

	caPin := caPinRe.FindString(output)
	if caPin == "" {
		return nil, fmt.Errorf("CA pin not found in the output of tctl status:\n%s", output)
	}

	m.mu.Lock()
	m.ownTarget = target
	m.clusterName = clusterName
	m.caPin = caPin
	m.mu.Unlock()

	state := sdk.StateMap(req.GetState())
	state["own_target_host"] = target.Host
	state["own_target_port"] = int64(target.Port)
	state["own_target_user"] = target.User
	state["cluster_name"] = clusterName
	state["ca_pin"] = caPin
	s, err := sdk.NewState(state)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

// deployTeleport installs/configures Teleport through ansible and places the
// provided TLS certificate -- safe to replay as is (idempotent playbook, only
// restarts if the TLS or the config changed).
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
		return "", fmt.Errorf("deploying teleport failed:\n%s", resp.GetOutput())
	}
	return resp.GetOutput(), nil
}
