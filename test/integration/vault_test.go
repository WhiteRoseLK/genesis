// SPDX-License-Identifier: Apache-2.0

//go:build docker

package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/WhiteRoseLK/genesis/internal/broker"
	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/secrets"
	"github.com/WhiteRoseLK/genesis/internal/testutil"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	dnsresolverv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/resolver/v1"
	osbasev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	pkiissuerv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/pki/issuer/v1"
	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// Vault, contrairement à chrony/coredns/powerdns, ne peut pas être installé
// via `apt`+systemd sur les conteneurs SSH jetables (Alpine, sans systemd)
// utilisés ailleurs dans ce dépôt : le mécanisme ansible/systemd réel est
// déjà prouvé (internal/broker/ansible_test.go) et ne peut de toute façon
// pas être réexercé ici sans une vraie VM Debian complète (hors périmètre
// sans accès Proxmox, docs/PROGRESS.md J5). vaultFakeAnsibleServer simule
// donc « ce qu'ansible aurait fait » en pilotant directement le VRAI
// conteneur hashicorp/vault avec les VRAIES variables (certificats TLS
// émis par le vrai step-ca, role_id/secret_id réels) que le module vault
// lui a transmises — tout le reste (init, unseal, pki_int signé par
// step-ca, KV v2, AppRole) tourne pour de vrai, contre un vrai Vault.

type vaultFakeComputeVMServer struct {
	computevmv1.UnimplementedComputeVMServer
	mu    sync.Mutex
	vms   map[string]*computevmv1.VM
	calls []*computevmv1.EnsureVMRequest
}

func newVaultFakeComputeVMServer() *vaultFakeComputeVMServer {
	return &vaultFakeComputeVMServer{vms: map[string]*computevmv1.VM{}}
}

func (f *vaultFakeComputeVMServer) EnsureVM(_ context.Context, req *computevmv1.EnsureVMRequest) (*computevmv1.VM, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if vm, ok := f.vms[req.GetName()]; ok {
		return vm, nil
	}
	vm := &computevmv1.VM{Id: "vm-" + req.GetName(), Name: req.GetName(), Ip: "127.0.0.1", Status: "running", SshPort: 22}
	f.vms[req.GetName()] = vm
	return vm, nil
}

func (f *vaultFakeComputeVMServer) DeleteVM(_ context.Context, req *computevmv1.DeleteVMRequest) (*computevmv1.DeleteVMResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.vms, req.GetName())
	return &computevmv1.DeleteVMResponse{}, nil
}

type vaultFakeOSBaseServer struct {
	osbasev1.UnimplementedBaseServer
}

func (vaultFakeOSBaseServer) SetNTP(context.Context, *osbasev1.SetNTPRequest) (*osbasev1.SetNTPResponse, error) {
	return &osbasev1.SetNTPResponse{}, nil
}
func (vaultFakeOSBaseServer) SetResolver(context.Context, *osbasev1.SetResolverRequest) (*osbasev1.SetResolverResponse, error) {
	return &osbasev1.SetResolverResponse{}, nil
}

type vaultFakeTimeNTPServer struct {
	timentpv1.UnimplementedTimeNTPServer
}

func (vaultFakeTimeNTPServer) Endpoint(context.Context, *timentpv1.Empty) (*timentpv1.EndpointInfo, error) {
	return &timentpv1.EndpointInfo{Address: "10.10.0.9", Port: 123}, nil
}

type vaultFakeDnsResolverServer struct {
	dnsresolverv1.UnimplementedDnsResolverServer
}

func (vaultFakeDnsResolverServer) Endpoint(context.Context, *dnsresolverv1.Empty) (*dnsresolverv1.EndpointInfo, error) {
	return &dnsresolverv1.EndpointInfo{Address: "10.10.0.8", Port: 53}, nil
}

const vaultTestPort = "8200"

type vaultFakeAnsibleServer struct {
	ansiblev1.UnimplementedAnsibleServer
	t         *testing.T
	mu        sync.Mutex
	dir       string
	container string
	calls     int
}

func (f *vaultFakeAnsibleServer) RunPlaybook(_ context.Context, req *ansiblev1.RunPlaybookRequest) (*ansiblev1.RunPlaybookResponse, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	playbook := string(req.GetPlaybookYaml())
	vars := req.GetVars().AsMap()

	if strings.Contains(playbook, "installer les prerequis") {
		out, err := f.deployVaultContainer(vars)
		if err != nil {
			return &ansiblev1.RunPlaybookResponse{Ok: false, Output: out + "\n" + err.Error()}, nil
		}
		return &ansiblev1.RunPlaybookResponse{Ok: true, Output: out}, nil
	}
	if strings.Contains(playbook, "installer les outils de vérification") {
		out, err := f.runVerification(vars)
		if err != nil {
			return &ansiblev1.RunPlaybookResponse{Ok: false, Output: out + "\n" + err.Error()}, nil
		}
		return &ansiblev1.RunPlaybookResponse{Ok: true, Output: out}, nil
	}
	return &ansiblev1.RunPlaybookResponse{Ok: false, Output: "playbook inconnu du fake"}, nil
}

// deployVaultContainer pilote directement Docker avec le VRAI certificat
// TLS reçu du module (émis par le vrai step-ca) — équivalent réel de ce
// qu'install_vault.yml ferait sur une VM (dépôt du cert, configuration,
// (re)démarrage du service).
func (f *vaultFakeAnsibleServer) deployVaultContainer(vars map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	certPEM, _ := vars["tls_cert_pem"].(string)
	keyPEM, _ := vars["tls_key_pem"].(string)
	if certPEM == "" || keyPEM == "" {
		return "", fmt.Errorf("vars tls_cert_pem/tls_key_pem manquantes")
	}

	if f.dir == "" {
		dir, err := os.MkdirTemp("", "genesis-vault-test-*")
		if err != nil {
			return "", err
		}
		if err := os.Chmod(dir, 0o777); err != nil {
			return "", err
		}
		if err := os.MkdirAll(dir+"/data", 0o777); err != nil {
			return "", err
		}
		// MkdirAll subit l'umask du process (souvent 022) : 0777 demandé
		// devient 0755 réel, insuffisant pour l'utilisateur interne non-root
		// du conteneur vault — chmod explicite après coup, même précaution
		// que core.ansible/v1 et modules/coredns (docs/PROGRESS.md J5/J6).
		if err := os.Chmod(dir+"/data", 0o777); err != nil {
			return "", err
		}
		f.dir = dir
	}
	if err := os.WriteFile(f.dir+"/vault-cert.pem", []byte(certPEM), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(f.dir+"/vault-key.pem", []byte(keyPEM), 0o644); err != nil {
		return "", err
	}
	hcl := `
storage "raft" {
  path = "/vault/data"
  node_id = "genesis-vault-1"
}
listener "tcp" {
  address = "0.0.0.0:8200"
  tls_cert_file = "/vault/config/vault-cert.pem"
  tls_key_file = "/vault/config/vault-key.pem"
}
api_addr = "https://127.0.0.1:` + vaultTestPort + `"
cluster_addr = "https://127.0.0.1:8201"
disable_mlock = true
`
	if err := os.WriteFile(f.dir+"/vault.hcl", []byte(hcl), 0o644); err != nil {
		return "", err
	}

	if f.container != "" {
		_ = exec.Command("docker", "rm", "-f", f.container).Run()
	}
	name := "genesis-vault-test"
	_ = exec.Command("docker", "rm", "-f", name).Run()
	cmd := exec.Command("docker", "run", "-d", "--name", name,
		"--cap-add=IPC_LOCK",
		"-p", "127.0.0.1:"+vaultTestPort+":8200",
		"-v", f.dir+":/vault/config",
		"-v", f.dir+"/data:/vault/data",
		"hashicorp/vault:2.1.1@sha256:47f14a6acb98f48d798a07df7c83f23a6e636e1cf724c5f8ff165cb32667a1e2", "server")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker run vault : %w", err)
	}
	f.container = strings.TrimSpace(string(out))
	return string(out), nil
}

func (f *vaultFakeAnsibleServer) cleanup() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.container != "" {
		_ = exec.Command("docker", "rm", "-f", f.container).Run()
	}
	if f.dir != "" {
		_ = os.RemoveAll(f.dir)
	}
}

// runVerification exécute réellement, en Go, ce que check_vault.yml ferait
// en shell sur une VM tierce (login AppRole, émission, vérification de
// chaîne, écriture/lecture KV) — même API, mêmes identifiants réels.
func (f *vaultFakeAnsibleServer) runVerification(vars map[string]any) (string, error) {
	vaultAddr, _ := vars["vault_addr"].(string)
	roleID, _ := vars["role_id"].(string)
	secretID, _ := vars["secret_id"].(string)
	pkiMount, _ := vars["pki_mount"].(string)
	pkiRole, _ := vars["pki_role"].(string)
	kvMount, _ := vars["kv_mount"].(string)
	kvPath, _ := vars["kv_test_path"].(string)
	kvValue, _ := vars["kv_test_value"].(string)
	rootCAPEM, _ := vars["root_ca_pem"].(string)

	api, err := newVaultClientForTest(vaultAddr, rootCAPEM)
	if err != nil {
		return "", err
	}
	token, err := api.appRoleLogin(context.Background(), roleID, secretID)
	if err != nil {
		return "", fmt.Errorf("login approle : %w", err)
	}
	issued, err := api.issueCert(context.Background(), token, pkiMount, pkiRole, "verify-probe.internal", nil, "")
	if err != nil {
		return "", fmt.Errorf("issue : %w", err)
	}
	if !verifyChainForTest(issued.ChainPEM, rootCAPEM) {
		return "", fmt.Errorf("CHAIN_INVALID")
	}
	if err := api.kvWrite(context.Background(), token, kvMount, kvPath, kvValue); err != nil {
		return "", fmt.Errorf("kv write : %w", err)
	}
	got, found, err := api.kvRead(context.Background(), token, kvMount, kvPath)
	if err != nil || !found {
		return "", fmt.Errorf("kv read : found=%v err=%w", found, err)
	}
	return fmt.Sprintf("CHAIN_OK\nKV_VALUE=%s", got), nil
}

type vaultTestHandle struct {
	client        *modulehost.Client
	token         string
	configured    *modulev1.StepResult
	pki           pkiissuerv1.PkiIssuerClient
	ansibleServer *vaultFakeAnsibleServer
	store         *secrets.FileStore
}

func launchVaultTest(t *testing.T) vaultTestHandle {
	t.Helper()
	ctx := context.Background()

	rt := testutil.RequireRuntime(t)
	store := newTestSecretsStore(t)
	registry := broker.NewRegistry()
	registry.SetNative("core.container/v1", broker.NativeContainer(rt))
	registry.SetNative("core.secrets/v1", broker.NativeSecrets(store))

	// step-ca réel : seul fournisseur pki.issuer/v1@seed crédible pour ce
	// test (le réimplémenter en fake reviendrait à réécrire step-ca).
	stepCABinary, stepCAManifest := buildModule(t, "step-ca")
	stepCAClient, err := modulehost.Launch(stepCABinary, stepCAManifest)
	if err != nil {
		t.Fatalf("modulehost.Launch(step-ca) : %v", err)
	}
	t.Cleanup(stepCAClient.Close)
	stepCAToken := registry.OpenSession(stepCAClient.Broker(), "step-ca", []string{"core.container/v1", "core.secrets/v1"})
	if _, err := stepCAClient.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: stepCAToken}); err != nil {
		t.Fatalf("Check(step-ca) : %v", err)
	}
	seedToken := registry.OpenSession(stepCAClient.Broker(), "step-ca", []string{"core.container/v1", "core.secrets/v1"})
	if _, err := stepCAClient.Module().SeedUp(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: seedToken}); err != nil {
		t.Fatalf("SeedUp(step-ca) : %v", err)
	}
	stepCAConn, err := stepCAClient.DispenseFunction("pki.issuer/v1")
	if err != nil {
		t.Fatalf("DispenseFunction(step-ca, pki.issuer/v1) : %v", err)
	}

	binaryPath, manifest := buildModule(t, "vault")
	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("modulehost.Launch(vault) : %v", err)
	}
	t.Cleanup(client.Close)

	vmServer := newVaultFakeComputeVMServer()
	ansibleServer := &vaultFakeAnsibleServer{t: t}
	t.Cleanup(ansibleServer.cleanup)

	sessionID := client.Broker().NextId()
	go client.Broker().AcceptAndServe(sessionID, func(opts []grpc.ServerOption) *grpc.Server {
		s := grpc.NewServer(opts...)
		computevmv1.RegisterComputeVMServer(s, vmServer)
		osbasev1.RegisterBaseServer(s, vaultFakeOSBaseServer{})
		ansiblev1.RegisterAnsibleServer(s, ansibleServer)
		timentpv1.RegisterTimeNTPServer(s, vaultFakeTimeNTPServer{})
		dnsresolverv1.RegisterDnsResolverServer(s, vaultFakeDnsResolverServer{})
		broker.NativeSecrets(store)("vault")(s)
		pkiissuerv1.RegisterPkiIssuerServer(s, &pkiIssuerForwarder{client: pkiissuerv1.NewPkiIssuerClient(stepCAConn)})
		return s
	})
	token := strconv.FormatUint(uint64(sessionID), 10)

	if _, err := client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token}); err != nil {
		t.Fatalf("Check(vault) : %v", err)
	}
	provisionResp, err := client.Module().Provision(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Provision(vault) : %v", err)
	}
	configureResp, err := client.Module().Configure(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: provisionResp.GetState()})
	if err != nil {
		t.Fatalf("Configure(vault) : %v", err)
	}
	if configureResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Configure(vault).Status = %v", configureResp.GetStatus())
	}

	conn, err := client.DispenseFunction("pki.issuer/v1")
	if err != nil {
		t.Fatalf("DispenseFunction(vault, pki.issuer/v1) : %v", err)
	}
	return vaultTestHandle{
		client: client, token: token, configured: configureResp,
		pki: pkiissuerv1.NewPkiIssuerClient(conn), ansibleServer: ansibleServer, store: store,
	}
}

// pkiIssuerForwarder relaie vers le vrai step-ca, pour donner à vault une
// session pki.issuer/v1@seed fonctionnelle sans dupliquer le broker réel
// (internal/engine, non exercé ici — cohérent avec les autres tests
// module-isolés de ce paquet).
type pkiIssuerForwarder struct {
	pkiissuerv1.UnimplementedPkiIssuerServer
	client pkiissuerv1.PkiIssuerClient
}

func (f *pkiIssuerForwarder) IssueCert(ctx context.Context, req *pkiissuerv1.IssueCertRequest) (*pkiissuerv1.Certificate, error) {
	return f.client.IssueCert(ctx, req)
}
func (f *pkiIssuerForwarder) SignCSR(ctx context.Context, req *pkiissuerv1.SignCSRRequest) (*pkiissuerv1.Certificate, error) {
	return f.client.SignCSR(ctx, req)
}
func (f *pkiIssuerForwarder) CAChain(ctx context.Context, req *pkiissuerv1.Empty) (*pkiissuerv1.CAChainResponse, error) {
	return f.client.CAChain(ctx, req)
}

// TestVaultConfiguresAndIssuesRealCertificates prouve, contre un vrai
// conteneur hashicorp/vault et un vrai step-ca, tout le cycle J7 :
// init 5/3, unseal, pki_int signé par step-ca (le scénario exact prouvé
// par TestStepCASignsThirdPartyIntermediate), KV v2, AppRole — puis
// IssueCert/SignCSR via vault lui-même, et Verify (émission + chaîne +
// KV) depuis le point de vue consommateur.
func TestVaultConfiguresAndIssuesRealCertificates(t *testing.T) {
	h := launchVaultTest(t)
	ctx := context.Background()

	chain, err := h.pki.CAChain(ctx, &pkiissuerv1.Empty{})
	if err != nil {
		t.Fatalf("CAChain(vault) : %v", err)
	}
	if chain.GetChainPem() == "" {
		t.Fatal("CAChain(vault) : chaîne vide")
	}

	issued, err := h.pki.IssueCert(ctx, &pkiissuerv1.IssueCertRequest{CommonName: "infra01.lab.internal"})
	if err != nil {
		t.Fatalf("IssueCert(vault) : %v", err)
	}
	if issued.GetPrivateKeyPem() == "" {
		t.Error("IssueCert(vault) : private_key_pem vide")
	}
	if !verifyChainForTest(issued.GetChainPem()+"\n"+chain.GetChainPem(), chain.GetChainPem()) {
		t.Error("IssueCert(vault) : chaîne invalide jusqu'à la racine step-ca")
	}

	if h.ansibleServer.calls < 1 {
		t.Error("install_vault.yml n'a jamais été envoyé")
	}
}

// TestVaultVerifyPassesFromThirdPartyVM exécute la vraie étape Verify du
// cycle de vie (docs08 : critère d'acceptation J7) — émission, validation
// de chaîne et lecture KV via AppRole, pas le root token.
func TestVaultVerifyPassesFromThirdPartyVM(t *testing.T) {
	h := launchVaultTest(t)
	ctx := context.Background()

	verifyResp, err := h.client.Module().Verify(ctx, &modulev1.StepRequest{
		RunId: "test", BrokerToken: h.token, State: h.configured.GetState(),
	})
	if err != nil {
		t.Fatalf("Verify(vault) : %v", err)
	}
	if verifyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Verify(vault).Status = %v", verifyResp.GetStatus())
	}
	if h.ansibleServer.calls < 2 {
		t.Errorf("check_vault.yml n'a pas été envoyé (appels ansible = %d)", h.ansibleServer.calls)
	}
}

// TestVaultHandoverReissuesCertAndRevokesRootToken prouve Handover
// (docs/07-mvp-modules.md : "réémission de son propre certificat...
// révocation du token root") : vault redevient joignable après le
// redéploiement TLS via son PROPRE pki_int, IssueCert continue de
// fonctionner (token AppRole, jamais affecté par la révocation du root
// token), et le root token révoqué est bien rejeté par Vault.
func TestVaultHandoverReissuesCertAndRevokesRootToken(t *testing.T) {
	h := launchVaultTest(t)
	ctx := context.Background()

	rootTokenBefore, err := h.store.Get(ctx, secrets.Ref("vault/root-token"))
	if err != nil {
		t.Fatalf("lecture directe du root token (file) : %v", err)
	}

	handoverResp, err := h.client.Module().Handover(ctx, &modulev1.StepRequest{
		RunId: "test", BrokerToken: h.token, State: h.configured.GetState(),
	})
	if err != nil {
		t.Fatalf("Handover(vault) : %v", err)
	}
	if handoverResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Handover(vault).Status = %v", handoverResp.GetStatus())
	}

	chain, err := h.pki.CAChain(ctx, &pkiissuerv1.Empty{})
	if err != nil {
		t.Fatalf("CAChain(vault) après Handover : %v", err)
	}
	issued, err := h.pki.IssueCert(ctx, &pkiissuerv1.IssueCertRequest{CommonName: "post-handover.lab.internal"})
	if err != nil {
		t.Fatalf("IssueCert(vault) après Handover (token AppRole) : %v", err)
	}
	if !verifyChainForTest(issued.GetChainPem()+"\n"+chain.GetChainPem(), chain.GetChainPem()) {
		t.Error("IssueCert(vault) après Handover : chaîne invalide")
	}

	api, err := newVaultClientForTest("https://127.0.0.1:"+vaultTestPort, chain.GetChainPem())
	if err != nil {
		t.Fatalf("client de vérification : %v", err)
	}
	status, _, err := api.request(ctx, http.MethodGet, "/v1/auth/token/lookup-self", rootTokenBefore.ExposeSecret(), nil)
	if err != nil {
		t.Fatalf("requête de vérification du root token : %v", err)
	}
	if status != http.StatusForbidden {
		t.Errorf("le root token révoqué répond encore avec le statut %d, attendu 403", status)
	}
}

// --- petit client HTTP Vault et vérification de chaîne, dupliqués depuis
// modules/vault/api.go et main.go : internal/ n'importe jamais modules/
// (règle non négociable du cœur), et l'inverse non plus — ce test simule
// ici « ce qu'ansible ferait », pas le module lui-même.

type vaultTestClient struct {
	baseURL string
	http    *http.Client
}

func newVaultClientForTest(baseURL, rootCAPEM string) (*vaultTestClient, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(rootCAPEM)) {
		return nil, fmt.Errorf("racine CA illisible")
	}
	return &vaultTestClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}},
	}, nil
}

func (c *vaultTestClient) request(ctx context.Context, method, path, token string, body any) (int, map[string]any, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if len(data) == 0 {
		return resp.StatusCode, nil, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return resp.StatusCode, nil, fmt.Errorf("réponse non JSON : %s", string(data))
	}
	return resp.StatusCode, parsed, nil
}

func (c *vaultTestClient) appRoleLogin(ctx context.Context, roleID, secretID string) (string, error) {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/auth/approle/login", "", map[string]any{"role_id": roleID, "secret_id": secretID})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("login approle : statut %d : %v", status, parsed)
	}
	auth, _ := parsed["auth"].(map[string]any)
	token, _ := auth["client_token"].(string)
	if token == "" {
		return "", fmt.Errorf("login approle : pas de client_token")
	}
	return token, nil
}

type testIssuedCert struct{ CertPEM, ChainPEM string }

func (c *vaultTestClient) issueCert(ctx context.Context, token, pkiMount, role, commonName string, sans []string, ttl string) (testIssuedCert, error) {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/"+pkiMount+"/issue/"+role, token, map[string]any{"common_name": commonName})
	if err != nil {
		return testIssuedCert{}, err
	}
	if status != http.StatusOK {
		return testIssuedCert{}, fmt.Errorf("issue : statut %d : %v", status, parsed)
	}
	d, _ := parsed["data"].(map[string]any)
	cert, _ := d["certificate"].(string)
	issuingCA, _ := d["issuing_ca"].(string)
	return testIssuedCert{CertPEM: cert, ChainPEM: cert + "\n" + issuingCA}, nil
}

func (c *vaultTestClient) kvWrite(ctx context.Context, token, mount, path, value string) error {
	status, parsed, err := c.request(ctx, http.MethodPost, "/v1/"+mount+"/data/"+path, token, map[string]any{"data": map[string]any{"value": value}})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("kv write : statut %d : %v", status, parsed)
	}
	return nil
}

func (c *vaultTestClient) kvRead(ctx context.Context, token, mount, path string) (string, bool, error) {
	status, parsed, err := c.request(ctx, http.MethodGet, "/v1/"+mount+"/data/"+path, token, nil)
	if err != nil {
		return "", false, err
	}
	if status == http.StatusNotFound {
		return "", false, nil
	}
	if status != http.StatusOK {
		return "", false, fmt.Errorf("kv read : statut %d : %v", status, parsed)
	}
	outer, _ := parsed["data"].(map[string]any)
	inner, _ := outer["data"].(map[string]any)
	if inner == nil {
		return "", false, nil
	}
	value, ok := inner["value"].(string)
	return value, ok, nil
}

// verifyChainForTest vérifie cryptographiquement que chainPEM (feuille +
// intermédiaire(s)) remonte à rootPEM.
func verifyChainForTest(chainPEM, rootPEM string) bool {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(rootPEM)) {
		return false
	}
	intermediates := x509.NewCertPool()
	var leaf *x509.Certificate
	rest := []byte(chainPEM)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return false
		}
		if leaf == nil {
			leaf = cert
			continue
		}
		intermediates.AddCert(cert)
	}
	if leaf == nil {
		return false
	}
	_, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	return err == nil
}
