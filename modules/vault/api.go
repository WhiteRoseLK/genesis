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

// vaultClient talks directly to Vault's REST API (docs/07-mvp-modules.md), the
// same principle as modules/powerdns/api.go and modules/proxmox/proxmoxapi: a
// module calls the API of the product it drives directly, and ansible is
// limited to system installation/configuration.
type vaultClient struct {
	baseURL string
	http    *http.Client
}

// newVaultClient trusts ONLY rootCAPEM (the step-ca root that signed Vault's
// server certificate), not the system store — Vault is its own internal
// service, never exposed publicly in this MVP (docs/01-vision-scope.md).
func newVaultClient(baseURL, rootCAPEM string) (*vaultClient, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(rootCAPEM)) {
		return nil, fmt.Errorf("unreadable CA root for the Vault client")
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
			return 0, nil, fmt.Errorf("encoding the Vault request: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("building the Vault request: %w", err)
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("vault request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("reading the Vault response %s %s: %w", method, path, err)
	}
	if len(data) == 0 {
		return resp.StatusCode, nil, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return resp.StatusCode, nil, fmt.Errorf("decoding the Vault response %s %s: %s", method, path, string(data))
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

// active reports whether this node is the active Raft leader — right after
// unsealing, a short moment passes before the election, even with a single
// node (observed by hand: the first calls fail with "local node not active but
// active cluster node not found").
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
		return nil, "", fmt.Errorf("vault init: status %d: %v", status, parsed)
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
		return true, fmt.Errorf("vault unseal: status %d: %v", status, parsed)
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
		return fmt.Errorf("mounting %q (%s): status %d: %v", path, engineType, status, parsed)
	}
	return nil
}

func (c *vaultClient) tuneMaxLeaseTTL(ctx context.Context, token, path, ttl string) error {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/sys/mounts/"+path+"/tune", token, map[string]any{"max_lease_ttl": ttl})
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("setting the max TTL of %q: status %d: %v", path, status, parsed)
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
		return "", fmt.Errorf("generating the intermediate CSR: status %d: %v", status, parsed)
	}
	csr, _ := dataField(parsed)["csr"].(string)
	if csr == "" {
		return "", fmt.Errorf("generating the intermediate CSR: response without csr")
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
		return fmt.Errorf("importing the signed intermediate: status %d: %v", status, parsed)
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
		return fmt.Errorf("configuring the PKI URLs: status %d: %v", status, parsed)
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
		return fmt.Errorf("creating PKI role %q: status %d: %v", role, status, parsed)
	}
	return nil
}

type issuedCert struct {
	CertPEM       string
	ChainPEM      string
	PrivateKeyPEM string
}

// issueCert separates IP SANs from DNS SANs: the Vault PKI API has two
// distinct fields (alt_names for DNS names, ip_sans for IP addresses) — an IP
// passed in alt_names is silently ignored (a real bug met while testing
// Handover: the target VM is addressed by IP, "127.0.0.1" in tests, never
// recognised as a SAN as long as it is not in ip_sans).
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
		return issuedCert{}, fmt.Errorf("issuing the certificate for %q: status %d: %v", commonName, status, parsed)
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
		return issuedCert{}, fmt.Errorf("signing the CSR: status %d: %v", status, parsed)
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
		return fmt.Errorf("enabling approle: status %d: %v", status, parsed)
	}
	return nil
}

func (c *vaultClient) writePolicy(ctx context.Context, token, name, policyHCL string) error {
	status, parsed, err := c.request(ctx, http.MethodPut, "/v1/sys/policies/acl/"+name, token, map[string]any{"policy": policyHCL})
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("writing policy %q: status %d: %v", name, status, parsed)
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
		return fmt.Errorf("creating AppRole role %q: status %d: %v", name, status, parsed)
	}
	return nil
}

func (c *vaultClient) readRoleID(ctx context.Context, token, name string) (string, error) {
	status, parsed, err := c.request(ctx, http.MethodGet, "/v1/auth/approle/role/"+name+"/role-id", token, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("reading the role_id of %q: status %d: %v", name, status, parsed)
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
		return "", fmt.Errorf("generating the secret_id of %q: status %d: %v", name, status, parsed)
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
		return "", fmt.Errorf("AppRole login: status %d: %v", status, parsed)
	}
	auth, _ := parsed["auth"].(map[string]any)
	token, _ := auth["client_token"].(string)
	if token == "" {
		return "", fmt.Errorf("AppRole login: response without client_token")
	}
	return token, nil
}

func (c *vaultClient) revokeSelf(ctx context.Context, token string) error {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/auth/token/revoke-self", token, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("revoking the token: status %d: %v", status, parsed)
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
		return fmt.Errorf("KV write %q: status %d: %v", path, status, parsed)
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
		return "", false, fmt.Errorf("KV read %q: status %d: %v", path, status, parsed)
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
