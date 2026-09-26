// SPDX-License-Identifier: Apache-2.0

// vault fournit pki.issuer/v1 et secrets.kv/v1 en phase cible
// (docs/07-modules-mvp.md) : Raft mono-nœud, TLS initial via
// pki.issuer/v1@seed (step-ca), moteur PKI intermédiaire (pki_int) signé
// par step-ca, KV v2, AppRole "genesis" — même principe que modules/powerdns
// (api.go parle directement à l'API REST du produit, ansible se limite à
// l'installation/configuration système).
//
// Portée assumée : comme coredns/powerdns, le client HTTP actif (URL, pool
// TLS, token AppRole) vit en mémoire dans le process du module, peuplé par
// Configure (dette, docs/PROGRESS.md).
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	secretsv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/secrets/v1"
	dnsresolverv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/resolver/v1"
	fleetagentv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/fleet/agent/v1"
	osbasev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	pkiissuerv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/pki/issuer/v1"
	secretskvv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/secrets/kv/v1"
	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

//go:embed playbooks/install_vault.yml
var installVaultPlaybook []byte

//go:embed playbooks/check_vault.yml
var checkVaultPlaybook []byte

const (
	defaultVMName      = "vault01"
	sshUser            = "genesis"
	sshKeyRefFmt       = "vault/%s/ssh-key"
	unsealKeysRef      = "vault/unseal-keys" // JSON array de 5 clés (docs06 : shamir-shares, recovery)
	rootTokenRef       = "vault/root-token"
	approleRoleIDRef   = "vault/approle-role-id"
	approleSecretIDRef = "vault/approle-secret-id"
	vaultAPIPort       = 8200
	pkiMount           = "pki_int"
	pkiRole            = "genesis"
	kvMount            = "genesis"
	appRoleName        = "genesis"
	verifierSuffix     = "-verify"
	unsealShares       = 5
	unsealThreshold    = 3
)

// genesisPolicy autorise le KV applicatif (docs03, exemple de manifest) et
// l'émission/signature via pki_int — sans ce dernier volet, IssueCert/
// SignCSR échoueraient avec "permission denied" en utilisant le token
// AppRole plutôt que le root token (bug réel trouvé en testant contre un
// vrai Vault avant d'écrire cette policy correctement).
const genesisPolicy = `
path "genesis/data/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
path "genesis/metadata/*" {
  capabilities = ["read", "list"]
}
path "pki_int/issue/*" {
  capabilities = ["create", "update"]
}
path "pki_int/sign/*" {
  capabilities = ["create", "update"]
}
`

// sshKeyPair reflète la valeur JSON du générateur GENERATOR_SSH_KEYPAIR
// côté cœur (internal/secrets.SSHKeyPair) — dupliqué ici par son contrat
// JSON, comme modules/chrony et modules/powerdns.
type sshKeyPair struct {
	PrivateKeyOpenSSH   string `json:"private_key_openssh"`
	PublicKeyAuthorized string `json:"public_key_authorized"`
}

type vaultModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest

	mu                sync.Mutex
	broker            *sdk.BrokerClient
	brokerToken       string
	vmClient          computevmv1.ComputeVMClient
	osBaseClient      osbasev1.BaseClient
	ansibleClient     ansiblev1.AnsibleClient
	secretsClient     secretsv1.SecretsClient
	timeNTPClient     timentpv1.TimeNTPClient
	dnsResolverClient dnsresolverv1.DnsResolverClient
	pkiSeedClient     pkiissuerv1.PkiIssuerClient
	fleetAgentClient  fleetagentv1.FleetAgentClient

	// Actif une fois Configure passé (docs/PROGRESS.md : dette, en mémoire
	// seulement).
	api          *vaultClient
	vmIP         string
	approleToken string
	rootCAPEM    string // racine + intermédiaire step-ca, pool de confiance TLS
	pkiIntPEM    string // certificat pki_int actif de vault lui-même
}

func (m *vaultModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *vaultModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *vaultModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *vaultModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	m.brokerToken = req.GetBrokerToken()
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_CONFORME}, nil
}

func (m *vaultModule) dial() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmClient != nil {
		return nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return fmt.Errorf("vault : aucune session de broker (Check n'a pas encore été appelé)")
	}
	conn, err := m.broker.Dial(m.brokerToken)
	if err != nil {
		return fmt.Errorf("connexion aux fonctions requises : %w", err)
	}
	m.vmClient = computevmv1.NewComputeVMClient(conn)
	m.osBaseClient = osbasev1.NewBaseClient(conn)
	m.ansibleClient = ansiblev1.NewAnsibleClient(conn)
	m.secretsClient = secretsv1.NewSecretsClient(conn)
	m.timeNTPClient = timentpv1.NewTimeNTPClient(conn)
	m.dnsResolverClient = dnsresolverv1.NewDnsResolverClient(conn)
	m.pkiSeedClient = pkiissuerv1.NewPkiIssuerClient(conn)
	m.fleetAgentClient = fleetagentv1.NewFleetAgentClient(conn)
	return nil
}

// installFleetAgents appelle fleet.agent/v1.Install(target) (docs/09-decisions.md
// ADR-017) : diffusé vers tout module « de parc » installé (ex. teleport),
// no-op silencieux si aucun n'est présent (fleet.agent/v1 est un requires
// optionnel — Unimplemented est alors la réponse normale du broker, pas
// une erreur), même méthode que modules/chrony et modules/powerdns.
func (m *vaultModule) installFleetAgents(ctx context.Context, target connTarget) error {
	_, err := m.fleetAgentClient.Install(ctx, &fleetagentv1.InstallRequest{
		Target: &fleetagentv1.Target{Host: target.Host, Port: target.Port, User: target.User, SshPrivateKey: target.PrivateKey},
	})
	if err != nil && status.Code(err) != codes.Unimplemented {
		return fmt.Errorf("fleet.agent/v1.Install : %w", err)
	}
	return nil
}

func vmName(req *modulev1.StepRequest) string {
	cfg := sdk.StateMap(req.GetConfig())
	if vm, ok := cfg["vm"].(map[string]any); ok {
		if name, ok := vm["name"].(string); ok && name != "" {
			return name
		}
	}
	return defaultVMName
}

func (m *vaultModule) sshKeyPair(ctx context.Context, name string) (sshKeyPair, error) {
	ref := fmt.Sprintf(sshKeyRefFmt, name)
	if _, err := m.secretsClient.Ensure(ctx, &secretsv1.EnsureRequest{
		Ref: ref, Generator: secretsv1.Generator_GENERATOR_SSH_KEYPAIR,
		Meta: &secretsv1.Meta{Owner: "vault", Consumers: []string{"vault"}, Kind: "ssh_keypair"},
	}); err != nil {
		return sshKeyPair{}, fmt.Errorf("génération de la paire SSH : %w", err)
	}
	resp, err := m.secretsClient.Get(ctx, &secretsv1.GetRequest{Ref: ref})
	if err != nil {
		return sshKeyPair{}, fmt.Errorf("lecture de la paire SSH : %w", err)
	}
	var pair sshKeyPair
	if err := json.Unmarshal([]byte(resp.GetValue()), &pair); err != nil {
		return sshKeyPair{}, fmt.Errorf("décodage de la paire SSH : %w", err)
	}
	return pair, nil
}

func (m *vaultModule) getSecret(ctx context.Context, ref string) (string, error) {
	resp, err := m.secretsClient.Get(ctx, &secretsv1.GetRequest{Ref: ref})
	if err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

func (m *vaultModule) putSecret(ctx context.Context, ref, value, kind string, recovery bool) error {
	_, err := m.secretsClient.Put(ctx, &secretsv1.PutRequest{
		Ref: ref, Value: value,
		Meta: &secretsv1.Meta{Owner: "vault", Consumers: []string{"vault"}, Kind: kind, Recovery: recovery},
	})
	return err
}

type connTarget struct {
	Host       string
	Port       int32
	User       string
	PrivateKey string
}

func (t connTarget) ansible() *ansiblev1.Target {
	return &ansiblev1.Target{Host: t.Host, Port: t.Port, User: t.User, SshPrivateKey: t.PrivateKey}
}

func (t connTarget) osBase() *osbasev1.Target {
	return &osbasev1.Target{Host: t.Host, Port: t.Port, User: t.User, SshPrivateKey: t.PrivateKey}
}

func targetFromState(req *modulev1.StepRequest, pair sshKeyPair) connTarget {
	state := sdk.StateMap(req.GetState())
	var port int64
	switch v := state["vm_ssh_port"].(type) {
	case int64:
		port = v
	case float64:
		port = int64(v)
	}
	return connTarget{Host: fmt.Sprint(state["vm_ip"]), Port: int32(port), User: sshUser, PrivateKey: pair.PrivateKeyOpenSSH}
}

// Provision crée (ou retrouve, EnsureVM est idempotent) la VM dédiée de vault.
func (m *vaultModule) Provision(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	pair, err := m.sshKeyPair(ctx, name)
	if err != nil {
		return nil, err
	}
	vm, err := m.vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name: name, Env: name, SshPublicKey: pair.PublicKeyAuthorized, User: sshUser,
	})
	if err != nil {
		return nil, fmt.Errorf("EnsureVM(%q) : %w", name, err)
	}
	state := sdk.StateMap(req.GetState())
	state["vm_name"] = name
	state["vm_ip"] = vm.GetIp()
	state["vm_ssh_port"] = int64(vm.GetSshPort())
	s, err := sdk.NewState(state)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func (m *vaultModule) Destroy(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	if _, err := m.vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: name}); err != nil {
		return nil, fmt.Errorf("DeleteVM(%q) : %w", name, err)
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	module := &vaultModule{manifest: mf.ToProto()}
	pkiServer := &vaultPkiServer{module: module}
	kvServer := &vaultKVServer{module: module}
	sdk.Serve(module,
		sdk.FunctionProvider{
			Name:     "pki.issuer/v1",
			Register: func(s *grpc.Server) { pkiissuerv1.RegisterPkiIssuerServer(s, pkiServer) },
		},
		sdk.FunctionProvider{
			Name:     "secrets.kv/v1",
			Register: func(s *grpc.Server) { secretskvv1.RegisterSecretsKVServer(s, kvServer) },
		},
	)
}
