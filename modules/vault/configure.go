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

// intermediateTTLSeconds (pki_int, signé par step-ca) doit strictement
// dépasser leafTTLSeconds (certificats TLS feuilles émis, que ce soit par
// step-ca ou par pki_int lui-même) — un émetteur ne peut jamais signer un
// certificat expirant après lui-même. Bug réel rencontré en testant
// Handover : les deux utilisaient la même durée, dépassée de quelques
// secondes par le décalage entre la création de pki_int (Configure) et la
// réémission du cert TLS (Handover, plus tard). Pas de renouvellement
// automatique planifié pour l'un ou l'autre (dette, docs/PROGRESS.md).
const (
	intermediateTTLSeconds = int64(365 * 24 * time.Hour / time.Second) // 1 an
	leafTTLSeconds         = int64(90 * 24 * time.Hour / time.Second)  // 90 jours
)

// Configure installe Vault, l'amorce (init/unseal), monte pki_int (signé
// par step-ca) et KV v2, active AppRole — tout le travail d'administration
// nécessite le root token, jamais exposé au-delà de cette étape (révoqué
// en Handover).
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

	rootChain, err := m.pkiSeedClient.CAChain(ctx, &pkiissuerv1.Empty{})
	if err != nil {
		return nil, fmt.Errorf("lecture de pki.issuer/v1@seed.CAChain : %w", err)
	}
	m.mu.Lock()
	m.rootCAPEM = rootChain.GetChainPem()
	m.mu.Unlock()

	tlsCert, err := m.pkiSeedClient.IssueCert(ctx, &pkiissuerv1.IssueCertRequest{
		CommonName: target.Host, Sans: []string{target.Host}, TtlSeconds: leafTTLSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("émission du certificat TLS de vault via pki.issuer/v1@seed : %w", err)
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
		return nil, fmt.Errorf("montage du moteur KV : %w", err)
	}
	roleID, secretID, err := m.ensureAppRole(ctx, api, rootToken)
	if err != nil {
		return nil, err
	}

	approleToken, err := api.appRoleLogin(ctx, roleID, secretID)
	if err != nil {
		return nil, fmt.Errorf("login AppRole : %w", err)
	}
	m.mu.Lock()
	m.approleToken = approleToken
	m.mu.Unlock()

	if err := m.installFleetAgents(ctx, target); err != nil {
		return nil, err
	}

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

// deployVault installe/configure Vault via ansible et dépose le certificat
// TLS fourni — rejoué tel quel en Handover pour redéployer un nouveau
// certificat (playbook idempotent, ne redémarre que si le TLS a changé).
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
		return fmt.Errorf("déploiement de vault a échoué :\n%s", resp.GetOutput())
	}
	return nil
}

func (m *vaultModule) currentRootCAPEM() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rootCAPEM
}

// waitForVault attend que le service vault réponde — le paquet vient
// d'être installé/démarré par ansible, quelques secondes de latence
// (même précaution que modules/chrony pour sshd/chronyd).
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
	return fmt.Errorf("vault ne répond pas après 60s : %w", lastErr)
}

// waitForActive attend l'élection du nœud Raft (même mono-nœud, un court
// instant suit le descellement avant que les opérations d'administration
// ne soient acceptées) — vérifié manuellement dans Docker.
func (m *vaultModule) waitForActive(ctx context.Context, api *vaultClient) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if active, err := api.active(ctx); err == nil && active {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("vault n'est jamais devenu le nœud actif après 30s")
}

// ensureInitializedAndUnsealed init 5/3 (docs07) une seule fois — les clés
// de descellement et le root token sont stockés via core.secrets
// (recovery:true, docs06 : jamais migrés vers vault lui-même). Un
// redémarrage relit ces clés et redescelle, sans réinitialiser.
func (m *vaultModule) ensureInitializedAndUnsealed(ctx context.Context, api *vaultClient) (rootToken string, err error) {
	initialized, sealed, err := api.sealStatus(ctx)
	if err != nil {
		return "", fmt.Errorf("lecture du statut de scellement : %w", err)
	}

	var unsealKeys []string
	if !initialized {
		unsealKeys, rootToken, err = api.init(ctx, unsealShares, unsealThreshold)
		if err != nil {
			return "", fmt.Errorf("initialisation de vault : %w", err)
		}
		keysJSON, err := json.Marshal(unsealKeys)
		if err != nil {
			return "", err
		}
		if err := m.putSecret(ctx, unsealKeysRef, string(keysJSON), "shamir-shares", true); err != nil {
			return "", fmt.Errorf("stockage des clés de descellement : %w", err)
		}
		if err := m.putSecret(ctx, rootTokenRef, rootToken, "token", true); err != nil {
			return "", fmt.Errorf("stockage du root token : %w", err)
		}
		sealed = true
	} else {
		keysJSON, err := m.getSecret(ctx, unsealKeysRef)
		if err != nil {
			return "", fmt.Errorf("lecture des clés de descellement : %w", err)
		}
		if err := json.Unmarshal([]byte(keysJSON), &unsealKeys); err != nil {
			return "", fmt.Errorf("décodage des clés de descellement : %w", err)
		}
		rootToken, err = m.getSecret(ctx, rootTokenRef)
		if err != nil {
			return "", fmt.Errorf("lecture du root token : %w", err)
		}
	}

	if sealed {
		for i := 0; i < unsealThreshold && i < len(unsealKeys); i++ {
			sealed, err = api.unseal(ctx, unsealKeys[i])
			if err != nil {
				return "", fmt.Errorf("descellement (clé %d) : %w", i, err)
			}
			if !sealed {
				break
			}
		}
	}
	return rootToken, nil
}

// ensurePKIEngine monte pki_int, génère son CSR, le fait signer par
// pki.issuer/v1@seed (is_ca=true — c'est l'exact scénario prouvé par
// internal/modulehost/stepca_test.go, TestStepCASignsThirdPartyIntermediate),
// l'importe, configure les URLs et le rôle d'émission (docs07 : "pki_int
// signé par la racine").
func (m *vaultModule) ensurePKIEngine(ctx context.Context, api *vaultClient, rootToken, vmIP string) error {
	if pemCA, err := m.getSecret(ctx, "vault/pki-intermediate-cert"); err == nil {
		m.mu.Lock()
		m.pkiIntPEM = pemCA
		m.mu.Unlock()
		return nil // déjà configuré (idempotent).
	}

	if err := api.ensureMount(ctx, rootToken, pkiMount, "pki", nil); err != nil {
		return fmt.Errorf("montage du moteur PKI : %w", err)
	}
	if err := api.tuneMaxLeaseTTL(ctx, rootToken, pkiMount, "87600h"); err != nil {
		return fmt.Errorf("réglage du TTL max de pki_int : %w", err)
	}
	csr, err := api.generateIntermediateCSR(ctx, rootToken, pkiMount, "Genesis Vault Intermediate CA")
	if err != nil {
		return fmt.Errorf("génération du CSR de l'intermédiaire vault : %w", err)
	}
	signed, err := m.pkiSeedClient.SignCSR(ctx, &pkiissuerv1.SignCSRRequest{
		CsrPem: csr, IsCa: true, PathLenConstraint: 0, TtlSeconds: intermediateTTLSeconds,
	})
	if err != nil {
		return fmt.Errorf("signature du CSR de l'intermédiaire vault par pki.issuer/v1@seed : %w", err)
	}
	if err := api.setSignedIntermediate(ctx, rootToken, pkiMount, signed.GetChainPem()); err != nil {
		return fmt.Errorf("import de l'intermédiaire signé : %w", err)
	}
	if err := api.configurePKIURLs(ctx, rootToken, pkiMount, fmt.Sprintf("https://%s:%d", vmIP, vaultAPIPort)); err != nil {
		return fmt.Errorf("configuration des URLs PKI : %w", err)
	}
	if err := api.ensurePKIRole(ctx, rootToken, pkiMount, pkiRole, "87600h"); err != nil {
		return fmt.Errorf("création du rôle PKI : %w", err)
	}
	if err := m.putSecret(ctx, "vault/pki-intermediate-cert", signed.GetCertPem(), "certificate", false); err != nil {
		return err
	}
	m.mu.Lock()
	m.pkiIntPEM = signed.GetCertPem()
	m.mu.Unlock()
	return nil
}

// ensureAppRole active AppRole, écrit la policy "genesis" (accès KV
// genesis/*, docs03 exemple de manifest) et le rôle associé, génère
// role_id/secret_id une seule fois (stockés via core.secrets, recovery:true —
// ce sont des identifiants d'authentification du cœur, pas des secrets
// applicatifs migrables).
func (m *vaultModule) ensureAppRole(ctx context.Context, api *vaultClient, rootToken string) (roleID, secretID string, err error) {
	if roleID, err = m.getSecret(ctx, approleRoleIDRef); err == nil {
		if secretID, err = m.getSecret(ctx, approleSecretIDRef); err == nil {
			return roleID, secretID, nil // déjà configuré (idempotent).
		}
	}

	if err := api.enableAppRole(ctx, rootToken); err != nil {
		return "", "", fmt.Errorf("activation d'approle : %w", err)
	}
	if err := api.writePolicy(ctx, rootToken, appRoleName, genesisPolicy); err != nil {
		return "", "", fmt.Errorf("écriture de la policy genesis : %w", err)
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

// Handover réémet le certificat TLS de vault via SON PROPRE pki_int
// (désormais actif) au lieu de celui de step-ca, le redéploie, puis révoque
// le root token (docs07 : "réémission de son propre certificat... révocation
// du token root").
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
		return nil, fmt.Errorf("Handover(vault) : lecture du root token : %w", err)
	}

	selfIssued, err := m.IssueCert(ctx, &pkiissuerv1.IssueCertRequest{
		CommonName: target.Host, Sans: []string{target.Host}, TtlSeconds: leafTTLSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("Handover(vault) : réémission du certificat via son propre pki_int : %w", err)
	}
	if err := m.deployVault(ctx, target, selfIssued); err != nil {
		return nil, fmt.Errorf("Handover(vault) : redéploiement du certificat : %w", err)
	}
	if err := m.waitForVault(ctx, api); err != nil {
		return nil, fmt.Errorf("Handover(vault) : %w", err)
	}
	// Le redéploiement redémarre le process Vault : le scellement est
	// toujours en mémoire (disable_mlock ne change rien à ça), donc
	// systématiquement reperdu au redémarrage — bug réel trouvé en testant
	// Handover, la révocation du root token échouait juste après avec
	// "Vault is sealed".
	if _, err := m.ensureInitializedAndUnsealed(ctx, api); err != nil {
		return nil, fmt.Errorf("Handover(vault) : redescellement après redéploiement : %w", err)
	}
	if err := m.waitForActive(ctx, api); err != nil {
		return nil, fmt.Errorf("Handover(vault) : %w", err)
	}

	if err := api.revokeSelf(ctx, rootToken); err != nil {
		return nil, fmt.Errorf("Handover(vault) : révocation du root token : %w", err)
	}

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

func (m *vaultModule) currentAPI() *vaultClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.api
}

// Verify prouve, depuis une VM tierce jetable, une émission + validation de
// chaîne réelle et une lecture KV via AppRole — pas le root token
// (docs/03-contrat-module.md règle 2, docs/07-modules-mvp.md).
func (m *vaultModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	state := sdk.StateMap(req.GetState())
	vmIP := fmt.Sprint(state["vm_ip"])

	roleID, err := m.getSecret(ctx, approleRoleIDRef)
	if err != nil {
		return nil, fmt.Errorf("Verify(vault) : %w", err)
	}
	secretID, err := m.getSecret(ctx, approleSecretIDRef)
	if err != nil {
		return nil, fmt.Errorf("Verify(vault) : %w", err)
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
		return nil, fmt.Errorf("EnsureVM(%q) (vérificateur) : %w", verifierName, err)
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
		return nil, fmt.Errorf("Verify(vault) a échoué :\n%s", resp.GetOutput())
	}
	if !strings.Contains(resp.GetOutput(), "CHAIN_OK") || !strings.Contains(resp.GetOutput(), "KV_VALUE=ok") {
		return nil, fmt.Errorf("Verify(vault) : sortie inattendue, attendu chaîne valide + KV_VALUE=ok :\n%s", resp.GetOutput())
	}

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}
