// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	dnsresolverv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/resolver/v1"
	osbasev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	pkiissuerv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/pki/issuer/v1"
	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// intermediateTTLSeconds (pki_int, signed by step-ca) must strictly exceed
// leafTTLSeconds (issued leaf TLS certificates, whether by step-ca or by
// pki_int itself) — an issuer can never sign a certificate that expires after
// itself. A real bug met while testing Handover: both used the same lifetime,
// exceeded by a few seconds because of the gap between the creation of pki_int
// (Configure) and the reissuance of the TLS cert (Handover, later). No
// scheduled automatic renewal for either (debt, docs/PROGRESS.md).
const (
	intermediateTTLSeconds = int64(365 * 24 * time.Hour / time.Second) // 1 an
	leafTTLSeconds         = int64(90 * 24 * time.Hour / time.Second)  // 90 days
)

// Configure installs Vault, bootstraps it (init/unseal), mounts pki_int
// (signed by step-ca) and KV v2, enables AppRole — all the administration work
// needs the root token, never exposed beyond this step (revoked in Handover).
func (m *vaultModule) Configure(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	pair, err := m.sshKeyPair(ctx, name)
	if err != nil {
		return nil, err
	}
	target := targetFromState(req, pair)

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

	rootChain, err := m.pkiSeedClient.CAChain(ctx, &pkiissuerv1.Empty{})
	if err != nil {
		return nil, fmt.Errorf("reading pki.issuer/v1@seed.CAChain: %w", err)
	}
	m.mu.Lock()
	m.rootCAPEM = rootChain.GetChainPem()
	m.mu.Unlock()

	tlsCert, err := m.pkiSeedClient.IssueCert(ctx, &pkiissuerv1.IssueCertRequest{
		CommonName: target.Host, Sans: []string{target.Host}, TtlSeconds: leafTTLSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("issuing vault's TLS certificate through pki.issuer/v1@seed: %w", err)
	}

	if err := m.deployVault(ctx, target, tlsCert); err != nil {
		return nil, err
	}

	api, err := newVaultClient(fmt.Sprintf("https://%s:%d", target.Host, vaultAPIPort), rootChain.GetChainPem())
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.api = api
	m.vmIP = target.Host
	m.mu.Unlock()

	if err := m.waitForVault(ctx, api); err != nil {
		return nil, err
	}

	rootToken, err := m.ensureInitializedAndUnsealed(ctx, api)
	if err != nil {
		return nil, err
	}
	if err := m.waitForActive(ctx, api); err != nil {
		return nil, err
	}

	if err := m.ensurePKIEngine(ctx, api, rootToken, target.Host); err != nil {
		return nil, err
	}
	if err := api.ensureMount(ctx, rootToken, kvMount, "kv", map[string]any{"version": "2"}); err != nil {
		return nil, fmt.Errorf("mounting the KV engine: %w", err)
	}
	roleID, secretID, err := m.ensureAppRole(ctx, api, rootToken)
	if err != nil {
		return nil, err
	}

	approleToken, err := api.appRoleLogin(ctx, roleID, secretID)
	if err != nil {
		return nil, fmt.Errorf("login AppRole: %w", err)
	}
	m.mu.Lock()
	m.approleToken = approleToken
	m.mu.Unlock()

	if err := m.installFleetAgents(ctx, target); err != nil {
		return nil, err
	}

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

// deployVault installs/configures Vault through ansible and places the
// provided TLS certificate — replayed as is in Handover to redeploy a new
// certificate (idempotent playbook, only restarts if the TLS changed).
func (m *vaultModule) deployVault(ctx context.Context, target connTarget, cert *pkiissuerv1.Certificate) error {
	vars, err := sdk.NewState(map[string]any{
		"tls_cert_pem": cert.GetChainPem() + "\n" + m.currentRootCAPEM(),
		"tls_key_pem":  cert.GetPrivateKeyPem(),
		"vm_ip":        target.Host,
	})
	if err != nil {
		return err
	}
	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target: target.ansible(), PlaybookYaml: installVaultPlaybook, Vars: vars,
	})
	if err != nil {
		return err
	}
	if !resp.GetOk() {
		return fmt.Errorf("deploying vault failed:\n%s", resp.GetOutput())
	}
	return nil
}

func (m *vaultModule) currentRootCAPEM() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rootCAPEM
}

// waitForVault waits for the vault service to answer — the package has just
// been installed/started by ansible, a few seconds of latency (the same
// precaution as modules/chrony for sshd/chronyd).
func (m *vaultModule) waitForVault(ctx context.Context, api *vaultClient) error {
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, _, err := api.sealStatus(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("vault is not answering after 60s: %w", lastErr)
}

// waitForActive waits for the Raft node election (even with a single node, a
// short moment follows unsealing before administration operations are
// accepted) — checked by hand in Docker.
func (m *vaultModule) waitForActive(ctx context.Context, api *vaultClient) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if active, err := api.active(ctx); err == nil && active {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("vault never became the active node after 30s")
}

// ensureInitializedAndUnsealed runs init 5/3 (doc 07) only once — the unseal
// keys and the root token are stored through core.secrets (recovery:true, doc
// 06: never migrated to vault itself). A restart reads these keys back and
// unseals again, without re-initialising.
func (m *vaultModule) ensureInitializedAndUnsealed(ctx context.Context, api *vaultClient) (rootToken string, err error) {
	initialized, sealed, err := api.sealStatus(ctx)
	if err != nil {
		return "", fmt.Errorf("reading the seal status: %w", err)
	}

	var unsealKeys []string
	if !initialized {
		unsealKeys, rootToken, err = api.init(ctx, unsealShares, unsealThreshold)
		if err != nil {
			return "", fmt.Errorf("initialisation de vault: %w", err)
		}
		keysJSON, err := json.Marshal(unsealKeys)
		if err != nil {
			return "", err
		}
		if err := m.putSecret(ctx, unsealKeysRef, string(keysJSON), "shamir-shares", true); err != nil {
			return "", fmt.Errorf("storing the unseal keys: %w", err)
		}
		if err := m.putSecret(ctx, rootTokenRef, rootToken, "token", true); err != nil {
			return "", fmt.Errorf("storing the root token: %w", err)
		}
		sealed = true
	} else {
		keysJSON, err := m.getSecret(ctx, unsealKeysRef)
		if err != nil {
			return "", fmt.Errorf("reading the unseal keys: %w", err)
		}
		if err := json.Unmarshal([]byte(keysJSON), &unsealKeys); err != nil {
			return "", fmt.Errorf("decoding the unseal keys: %w", err)
		}
		rootToken, err = m.getSecret(ctx, rootTokenRef)
		if err != nil {
			return "", fmt.Errorf("reading the root token: %w", err)
		}
	}

	if sealed {
		for i := 0; i < unsealThreshold && i < len(unsealKeys); i++ {
			sealed, err = api.unseal(ctx, unsealKeys[i])
			if err != nil {
				return "", fmt.Errorf("unsealing (key %d): %w", i, err)
			}
			if !sealed {
				break
			}
		}
	}
	return rootToken, nil
}

// ensurePKIEngine mounts pki_int, generates its CSR, has it signed by
// pki.issuer/v1@seed (is_ca=true — exactly the scenario proven by
// internal/modulehost/stepca_test.go, TestStepCASignsThirdPartyIntermediate),
// imports it, configures the URLs and the issuing role (doc 07: "pki_int
// signed by the root").
func (m *vaultModule) ensurePKIEngine(ctx context.Context, api *vaultClient, rootToken, vmIP string) error {
	if pemCA, err := m.getSecret(ctx, "vault/pki-intermediate-cert"); err == nil {
		m.mu.Lock()
		m.pkiIntPEM = pemCA
		m.mu.Unlock()
		return nil // already configured (idempotent).
	}

	if err := api.ensureMount(ctx, rootToken, pkiMount, "pki", nil); err != nil {
		return fmt.Errorf("mounting the PKI engine: %w", err)
	}
	if err := api.tuneMaxLeaseTTL(ctx, rootToken, pkiMount, "87600h"); err != nil {
		return fmt.Errorf("setting the max TTL of pki_int: %w", err)
	}
	csr, err := api.generateIntermediateCSR(ctx, rootToken, pkiMount, "Genesis Vault Intermediate CA")
	if err != nil {
		return fmt.Errorf("generating the CSR of the vault intermediate: %w", err)
	}
	signed, err := m.pkiSeedClient.SignCSR(ctx, &pkiissuerv1.SignCSRRequest{
		CsrPem: csr, IsCa: true, PathLenConstraint: 0, TtlSeconds: intermediateTTLSeconds,
	})
	if err != nil {
		return fmt.Errorf("signing the CSR of the vault intermediate with pki.issuer/v1@seed: %w", err)
	}
	if err := api.setSignedIntermediate(ctx, rootToken, pkiMount, signed.GetChainPem()); err != nil {
		return fmt.Errorf("importing the signed intermediate: %w", err)
	}
	if err := api.configurePKIURLs(ctx, rootToken, pkiMount, fmt.Sprintf("https://%s:%d", vmIP, vaultAPIPort)); err != nil {
		return fmt.Errorf("configuring the PKI URLs: %w", err)
	}
	if err := api.ensurePKIRole(ctx, rootToken, pkiMount, pkiRole, "87600h"); err != nil {
		return fmt.Errorf("creating the PKI role: %w", err)
	}
	if err := m.putSecret(ctx, "vault/pki-intermediate-cert", signed.GetCertPem(), "certificate", false); err != nil {
		return err
	}
	m.mu.Lock()
	m.pkiIntPEM = signed.GetCertPem()
	m.mu.Unlock()
	return nil
}

// ensureAppRole enables AppRole, writes the "genesis" policy (KV access to
// genesis/*, the doc 03 manifest example) and the associated role, and
// generates role_id/secret_id only once (stored through core.secrets,
// recovery:true — these are the core's authentication credentials, not
// migratable application secrets).
func (m *vaultModule) ensureAppRole(ctx context.Context, api *vaultClient, rootToken string) (roleID, secretID string, err error) {
	if roleID, err = m.getSecret(ctx, approleRoleIDRef); err == nil {
		if secretID, err = m.getSecret(ctx, approleSecretIDRef); err == nil {
			return roleID, secretID, nil // already configured (idempotent).
		}
	}

	if err := api.enableAppRole(ctx, rootToken); err != nil {
		return "", "", fmt.Errorf("enabling approle: %w", err)
	}
	if err := api.writePolicy(ctx, rootToken, appRoleName, genesisPolicy); err != nil {
		return "", "", fmt.Errorf("writing the genesis policy: %w", err)
	}
	if err := api.ensureAppRoleRole(ctx, rootToken, appRoleName, []string{appRoleName}, "1h"); err != nil {
		return "", "", err
	}
	roleID, err = api.readRoleID(ctx, rootToken, appRoleName)
	if err != nil {
		return "", "", err
	}
	secretID, err = api.generateSecretID(ctx, rootToken, appRoleName)
	if err != nil {
		return "", "", err
	}
	if err := m.putSecret(ctx, approleRoleIDRef, roleID, "approle-role-id", true); err != nil {
		return "", "", err
	}
	if err := m.putSecret(ctx, approleSecretIDRef, secretID, "approle-secret-id", true); err != nil {
		return "", "", err
	}
	return roleID, secretID, nil
}

// Handover reissues vault's TLS certificate through ITS OWN pki_int (now
// active) instead of step-ca's, redeploys it, then revokes the root token (doc
// 07: "reissues its own certificate... revokes the root token").
func (m *vaultModule) Handover(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	pair, err := m.sshKeyPair(ctx, name)
	if err != nil {
		return nil, err
	}
	target := targetFromState(req, pair)

	api := m.currentAPI()
	rootToken, err := m.getSecret(ctx, rootTokenRef)
	if err != nil {
		return nil, fmt.Errorf("Handover(vault): reading the root token: %w", err)
	}

	selfIssued, err := m.IssueCert(ctx, &pkiissuerv1.IssueCertRequest{
		CommonName: target.Host, Sans: []string{target.Host}, TtlSeconds: leafTTLSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("Handover(vault): reissuing the certificate through its own pki_int: %w", err)
	}
	if err := m.deployVault(ctx, target, selfIssued); err != nil {
		return nil, fmt.Errorf("Handover(vault): redeploying the certificate: %w", err)
	}
	if err := m.waitForVault(ctx, api); err != nil {
		return nil, fmt.Errorf("Handover(vault): %w", err)
	}
	// Redeploying restarts the Vault process: the seal state always lives in
	// memory (disable_mlock changes nothing there), so it is lost on every
	// restart — a real bug found while testing Handover, where revoking the
	// root token failed right afterwards with "Vault is sealed".
	if _, err := m.ensureInitializedAndUnsealed(ctx, api); err != nil {
		return nil, fmt.Errorf("Handover(vault): unsealing again after the redeployment: %w", err)
	}
	if err := m.waitForActive(ctx, api); err != nil {
		return nil, fmt.Errorf("Handover(vault): %w", err)
	}

	if err := api.revokeSelf(ctx, rootToken); err != nil {
		return nil, fmt.Errorf("Handover(vault): revoking the root token: %w", err)
	}

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

func (m *vaultModule) currentAPI() *vaultClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.api
}

// Verify proves, from a disposable third-party VM, a real issuance + chain
// validation and a KV read through the AppRole — not the root token
// (docs/03-module-contract.md rule 2, docs/07-mvp-modules.md).
func (m *vaultModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	state := sdk.StateMap(req.GetState())
	vmIP := fmt.Sprint(state["vm_ip"])

	roleID, err := m.getSecret(ctx, approleRoleIDRef)
	if err != nil {
		return nil, fmt.Errorf("Verify(vault): %w", err)
	}
	secretID, err := m.getSecret(ctx, approleSecretIDRef)
	if err != nil {
		return nil, fmt.Errorf("Verify(vault): %w", err)
	}

	verifierName := name + verifierSuffix
	pair, err := m.sshKeyPair(ctx, verifierName)
	if err != nil {
		return nil, err
	}
	verifierVM, err := m.vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name: verifierName, Env: verifierName, SshPublicKey: pair.PublicKeyAuthorized, User: sshUser,
	})
	if err != nil {
		return nil, fmt.Errorf("EnsureVM(%q) (verifier): %w", verifierName, err)
	}
	defer func() {
		_, _ = m.vmClient.DeleteVM(context.Background(), &computevmv1.DeleteVMRequest{Name: verifierName})
	}()

	target := &ansiblev1.Target{
		Host: verifierVM.GetIp(), Port: verifierVM.GetSshPort(), User: sshUser, SshPrivateKey: pair.PrivateKeyOpenSSH,
	}
	vars, err := sdk.NewState(map[string]any{
		"vault_addr":        fmt.Sprintf("https://%s:%d", vmIP, vaultAPIPort),
		"role_id":           roleID,
		"secret_id":         secretID,
		"pki_mount":         pkiMount,
		"pki_role":          pkiRole,
		"probe_common_name": "verify-probe.internal",
		"kv_mount":          kvMount,
		"kv_test_path":      "verify-probe",
		"kv_test_value":     "ok",
		"root_ca_pem":       m.currentRootCAPEM(),
	})
	if err != nil {
		return nil, err
	}
	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target: target, PlaybookYaml: checkVaultPlaybook, Vars: vars,
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetOk() {
		return nil, fmt.Errorf("Verify(vault) failed:\n%s", resp.GetOutput())
	}
	if !strings.Contains(resp.GetOutput(), "CHAIN_OK") || !strings.Contains(resp.GetOutput(), "KV_VALUE=ok") {
		return nil, fmt.Errorf("Verify(vault): unexpected output, expected a valid chain + KV_VALUE=ok:\n%s", resp.GetOutput())
	}

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}
