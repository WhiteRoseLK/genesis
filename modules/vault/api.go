// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// vaultClient parle directement à l'API REST de Vault (docs/07-mvp-modules.md),
// même principe que modules/powerdns/api.go et modules/proxmox/proxmoxapi :
// un module appelle l'API du produit qu'il pilote directement, ansible se
// limite à l'installation/configuration système.
type vaultClient struct {
	baseURL string
	http    *http.Client
}

// newVaultClient fait confiance UNIQUEMENT à rootCAPEM (la racine step-ca
// qui a signé le certificat serveur de Vault), pas au magasin système —
// Vault est son propre service interne, jamais exposé publiquement dans ce
// MVP (docs/01-vision-scope.md).
func newVaultClient(baseURL, rootCAPEM string) (*vaultClient, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(rootCAPEM)) {
		return nil, fmt.Errorf("racine CA illisible pour le client Vault")
	}
	return &vaultClient{
		baseURL: baseURL,
		http: &http.Client{
			Timeout:   20 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}},
		},
	}, nil
}

func (c *vaultClient) request(ctx context.Context, method, path, token string, body any) (int, map[string]any, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encodage de la requête Vault : %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("construction de la requête Vault : %w", err)
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("requête Vault %s %s : %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("lecture de la réponse Vault %s %s : %w", method, path, err)
	}
	if len(data) == 0 {
		return resp.StatusCode, nil, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return resp.StatusCode, nil, fmt.Errorf("décodage de la réponse Vault %s %s : %s", method, path, string(data))
	}
	return resp.StatusCode, parsed, nil
}

func dataField(parsed map[string]any) map[string]any {
	if parsed == nil {
		return nil
	}
	d, _ := parsed["data"].(map[string]any)
	return d
}

func (c *vaultClient) sealStatus(ctx context.Context) (initialized, sealed bool, err error) {
	_, parsed, err := c.request(ctx, http.MethodGet, "/v1/sys/seal-status", "", nil)
	if err != nil {
		return false, false, err
	}
	initialized, _ = parsed["initialized"].(bool)
	sealed, _ = parsed["sealed"].(bool)
	return initialized, sealed, nil
}

// active indique si ce nœud est le leader Raft actif — juste après le
// descellement, un court instant s'écoule avant l'élection même en
// mono-nœud (observé manuellement : les premiers appels échouent avec
// "local node not active but active cluster node not found").
func (c *vaultClient) active(ctx context.Context) (bool, error) {
	status, _, err := c.request(ctx, http.MethodGet, "/v1/sys/health", "", nil)
	if err != nil {
		return false, err
	}
	return status == http.StatusOK, nil
}

func (c *vaultClient) init(ctx context.Context, shares, threshold int) (unsealKeys []string, rootToken string, err error) {
	status, parsed, err := c.request(ctx, http.MethodPut, "/v1/sys/init", "", map[string]any{
		"secret_shares": shares, "secret_threshold": threshold,
	})
	if err != nil {
		return nil, "", err
	}
	if status != http.StatusOK {
		return nil, "", fmt.Errorf("init Vault : statut %d : %v", status, parsed)
	}
	keysRaw, _ := parsed["keys_base64"].([]any)
	keys := make([]string, 0, len(keysRaw))
	for _, k := range keysRaw {
		keys = append(keys, fmt.Sprint(k))
	}
	rootToken, _ = parsed["root_token"].(string)
	return keys, rootToken, nil
}

func (c *vaultClient) unseal(ctx context.Context, key string) (sealed bool, err error) {
	status, parsed, err := c.request(ctx, http.MethodPut, "/v1/sys/unseal", "", map[string]any{"key": key})
	if err != nil {
		return true, err
	}
	if status != http.StatusOK {
		return true, fmt.Errorf("unseal Vault : statut %d : %v", status, parsed)
	}
	sealed, _ = parsed["sealed"].(bool)
	return sealed, nil
}

func (c *vaultClient) mountExists(ctx context.Context, token, path string) (bool, error) {
	status, _, err := c.request(ctx, http.MethodGet, "/v1/sys/mounts/"+path+"/tune", token, nil)
	if err != nil {
		return false, err
	}
	return status == http.StatusOK, nil
}

func (c *vaultClient) ensureMount(ctx context.Context, token, path, engineType string, options map[string]any) error {
	exists, err := c.mountExists(ctx, token, path)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	body := map[string]any{"type": engineType}
	if options != nil {
		body["options"] = options
	}
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/sys/mounts/"+path, token, body)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("montage de %q (%s) : statut %d : %v", path, engineType, status, parsed)
	}
	return nil
}

func (c *vaultClient) tuneMaxLeaseTTL(ctx context.Context, token, path, ttl string) error {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/sys/mounts/"+path+"/tune", token, map[string]any{"max_lease_ttl": ttl})
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("réglage du TTL max de %q : statut %d : %v", path, status, parsed)
	}
	return nil
}

func (c *vaultClient) generateIntermediateCSR(ctx context.Context, token, pkiPath, commonName string) (string, error) {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/"+pkiPath+"/intermediate/generate/internal", token, map[string]any{
		"common_name": commonName, "key_type": "ec", "key_bits": 256,
	})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("génération du CSR intermédiaire : statut %d : %v", status, parsed)
	}
	csr, _ := dataField(parsed)["csr"].(string)
	if csr == "" {
		return "", fmt.Errorf("génération du CSR intermédiaire : réponse sans csr")
	}
	return csr, nil
}

func (c *vaultClient) setSignedIntermediate(ctx context.Context, token, pkiPath, certPEM string) error {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/"+pkiPath+"/intermediate/set-signed", token, map[string]any{
		"certificate": certPEM,
	})
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("import de l'intermédiaire signé : statut %d : %v", status, parsed)
	}
	return nil
}

func (c *vaultClient) configurePKIURLs(ctx context.Context, token, pkiPath, baseURL string) error {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/"+pkiPath+"/config/urls", token, map[string]any{
		"issuing_certificates":    []string{baseURL + "/v1/" + pkiPath + "/ca"},
		"crl_distribution_points": []string{baseURL + "/v1/" + pkiPath + "/crl"},
	})
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("configuration des URLs PKI : statut %d : %v", status, parsed)
	}
	return nil
}

func (c *vaultClient) ensurePKIRole(ctx context.Context, token, pkiPath, role string, maxTTL string) error {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/"+pkiPath+"/roles/"+role, token, map[string]any{
		"allow_any_name": true, "allow_ip_sans": true, "max_ttl": maxTTL,
	})
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("création du rôle PKI %q : statut %d : %v", role, status, parsed)
	}
	return nil
}

type issuedCert struct {
	CertPEM       string
	ChainPEM      string
	PrivateKeyPEM string
}

// issueCert sépare les SAN IP des SAN DNS : l'API Vault PKI a deux champs
// distincts (alt_names pour les noms DNS, ip_sans pour les adresses IP) —
// une IP passée dans alt_names est silencieusement ignorée (bug réel
// rencontré en testant Handover : la VM cible est adressée par IP,
// "127.0.0.1" en test, jamais reconnue comme SAN tant qu'elle n'est pas
// dans ip_sans).
func (c *vaultClient) issueCert(ctx context.Context, token, pkiPath, role, commonName string, sans []string, ttl string) (issuedCert, error) {
	body := map[string]any{"common_name": commonName}
	var dnsNames, ipAddrs []string
	for _, san := range sans {
		if net.ParseIP(san) != nil {
			ipAddrs = append(ipAddrs, san)
		} else {
			dnsNames = append(dnsNames, san)
		}
	}
	if len(dnsNames) > 0 {
		body["alt_names"] = joinStrings(dnsNames, ",")
	}
	if len(ipAddrs) > 0 {
		body["ip_sans"] = joinStrings(ipAddrs, ",")
	}
	if ttl != "" {
		body["ttl"] = ttl
	}
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/"+pkiPath+"/issue/"+role, token, body)
	if err != nil {
		return issuedCert{}, err
	}
	if status != http.StatusOK {
		return issuedCert{}, fmt.Errorf("émission du certificat pour %q : statut %d : %v", commonName, status, parsed)
	}
	d := dataField(parsed)
	cert, _ := d["certificate"].(string)
	issuingCA, _ := d["issuing_ca"].(string)
	key, _ := d["private_key"].(string)
	return issuedCert{CertPEM: cert, ChainPEM: cert + "\n" + issuingCA, PrivateKeyPEM: key}, nil
}

func (c *vaultClient) signCSR(ctx context.Context, token, pkiPath, role, csrPEM, commonName, ttl string) (issuedCert, error) {
	body := map[string]any{"csr": csrPEM}
	if commonName != "" {
		body["common_name"] = commonName
	}
	if ttl != "" {
		body["ttl"] = ttl
	}
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/"+pkiPath+"/sign/"+role, token, body)
	if err != nil {
		return issuedCert{}, err
	}
	if status != http.StatusOK {
		return issuedCert{}, fmt.Errorf("signature du CSR : statut %d : %v", status, parsed)
	}
	d := dataField(parsed)
	cert, _ := d["certificate"].(string)
	issuingCA, _ := d["issuing_ca"].(string)
	return issuedCert{CertPEM: cert, ChainPEM: cert + "\n" + issuingCA}, nil
}

func (c *vaultClient) enableAppRole(ctx context.Context, token string) error {
	status, parsed, err := c.request(ctx, http.MethodGet, "/v1/sys/auth", token, nil)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		if _, ok := parsed["approle/"]; ok {
			return nil
		}
	}
	status, parsed, err = c.request(ctx, http.MethodPost, "/v1/sys/auth/approle", token, map[string]any{"type": "approle"})
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("activation d'approle : statut %d : %v", status, parsed)
	}
	return nil
}

func (c *vaultClient) writePolicy(ctx context.Context, token, name, policyHCL string) error {
	status, parsed, err := c.request(ctx, http.MethodPut, "/v1/sys/policies/acl/"+name, token, map[string]any{"policy": policyHCL})
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("écriture de la policy %q : statut %d : %v", name, status, parsed)
	}
	return nil
}

func (c *vaultClient) ensureAppRoleRole(ctx context.Context, token, name string, policies []string, tokenTTL string) error {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/auth/approle/role/"+name, token, map[string]any{
		"token_policies": policies, "token_ttl": tokenTTL, "token_max_ttl": tokenTTL,
	})
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("création du rôle AppRole %q : statut %d : %v", name, status, parsed)
	}
	return nil
}

func (c *vaultClient) readRoleID(ctx context.Context, token, name string) (string, error) {
	status, parsed, err := c.request(ctx, http.MethodGet, "/v1/auth/approle/role/"+name+"/role-id", token, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("lecture du role_id de %q : statut %d : %v", name, status, parsed)
	}
	roleID, _ := dataField(parsed)["role_id"].(string)
	return roleID, nil
}

func (c *vaultClient) generateSecretID(ctx context.Context, token, name string) (string, error) {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/auth/approle/role/"+name+"/secret-id", token, map[string]any{})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("génération du secret_id de %q : statut %d : %v", name, status, parsed)
	}
	secretID, _ := dataField(parsed)["secret_id"].(string)
	return secretID, nil
}

func (c *vaultClient) appRoleLogin(ctx context.Context, roleID, secretID string) (string, error) {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/auth/approle/login", "", map[string]any{
		"role_id": roleID, "secret_id": secretID,
	})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("login AppRole : statut %d : %v", status, parsed)
	}
	auth, _ := parsed["auth"].(map[string]any)
	token, _ := auth["client_token"].(string)
	if token == "" {
		return "", fmt.Errorf("login AppRole : réponse sans client_token")
	}
	return token, nil
}

func (c *vaultClient) revokeSelf(ctx context.Context, token string) error {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/auth/token/revoke-self", token, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("révocation du token : statut %d : %v", status, parsed)
	}
	return nil
}

func (c *vaultClient) kvWrite(ctx context.Context, token, mount, path, value string) error {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/"+mount+"/data/"+path, token, map[string]any{
		"data": map[string]any{"value": value},
	})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("écriture KV %q : statut %d : %v", path, status, parsed)
	}
	return nil
}

func (c *vaultClient) kvRead(ctx context.Context, token, mount, path string) (value string, found bool, err error) {
	status, parsed, err := c.request(ctx, http.MethodGet, "/v1/"+mount+"/data/"+path, token, nil)
	if err != nil {
		return "", false, err
	}
	if status == http.StatusNotFound {
		return "", false, nil
	}
	if status != http.StatusOK {
		return "", false, fmt.Errorf("lecture KV %q : statut %d : %v", path, status, parsed)
	}
	d := dataField(dataField(parsed))
	if d == nil {
		return "", false, nil
	}
	value, ok := d["value"].(string)
	return value, ok, nil
}

func joinStrings(ss []string, sep string) string {
	out := ss[0]
	for _, s := range ss[1:] {
		out += sep + s
	}
	return out
}
