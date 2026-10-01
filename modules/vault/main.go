// SPDX-License-Identifier: Apache-2.0

// vault provides pki.issuer/v1 and secrets.kv/v1 in the target phase
// (docs/07-mvp-modules.md): single-node Raft, initial TLS through
// pki.issuer/v1@seed (step-ca), an intermediate PKI engine (pki_int) signed by
// step-ca, KV v2, the "genesis" AppRole — the same principle as
// modules/powerdns (api.go talks directly to the product's REST API, ansible
// is limited to system installation/configuration).
//
// Accepted scope: like coredns/powerdns, the active HTTP client (URL, TLS
// pool, AppRole token) lives in memory in the module process, populated by
// Configure (debt, docs/PROGRESS.md).
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
	accesssshv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/access/ssh/v1"
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
	unsealKeysRef      = "vault/unseal-keys" // JSON array of 5 keys (doc 06: shamir-shares, recovery)
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
	teleportAgentPort  = 3022
)

// genesisPolicy allows the application KV (doc 03, manifest example) and
// issuing/signing through pki_int — without the latter, IssueCert/SignCSR
// would fail with "permission denied" when using the AppRole token rather than
// the root token (a real bug found while testing against a real Vault, before
// this policy was written correctly).
const genesisPolicy = `
path "genesis/data/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
path "genesis/metadata/*" {
  capabilities = ["read", "list"]
}
path "genesis/metadata" {
  capabilities = ["list"]
}
path "pki_int/issue/*" {
  capabilities = ["create", "update"]
}
path "pki_int/sign/*" {
  capabilities = ["create", "update"]
}
`

// sshKeyPair mirrors the JSON value of the core's GENERATOR_SSH_KEYPAIR
// generator (internal/secrets.SSHKeyPair) — duplicated here from its JSON
// contract, like modules/chrony and modules/powerdns.
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
	accessSSHClient   accesssshv1.AccessSSHClient

	// Active once Configure has passed (docs/PROGRESS.md: debt, in memory
	// only).
	api          *vaultClient
	vmIP         string
	approleToken string
	rootCAPEM    string // step-ca root + intermediate, TLS trust pool
	pkiIntPEM    string // vault's own active pki_int certificate
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
	flags := sdk.StateMap(req.GetState())
	if boolFlag(flags, "handed_over") || boolFlag(flags, "verified") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_TODO}, nil
}

func boolFlag(flags map[string]any, key string) bool {
	v, _ := flags[key].(bool)
	return v
}

func (m *vaultModule) dial() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmClient != nil {
		return nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return fmt.Errorf("vault: no broker session (Check has not been called yet)")
	}
	conn, err := m.broker.Dial(m.brokerToken)
	if err != nil {
		return fmt.Errorf("connecting to the required functions: %w", err)
	}
	m.vmClient = computevmv1.NewComputeVMClient(conn)
	m.osBaseClient = osbasev1.NewBaseClient(conn)
	m.ansibleClient = ansiblev1.NewAnsibleClient(conn)
	m.secretsClient = secretsv1.NewSecretsClient(conn)
	m.timeNTPClient = timentpv1.NewTimeNTPClient(conn)
	m.dnsResolverClient = dnsresolverv1.NewDnsResolverClient(conn)
	m.pkiSeedClient = pkiissuerv1.NewPkiIssuerClient(conn)
	m.fleetAgentClient = fleetagentv1.NewFleetAgentClient(conn)
	m.accessSSHClient = accesssshv1.NewAccessSSHClient(conn)
	return nil
}

// installFleetAgents calls fleet.agent/v1.Install(target)
// (docs/09-decisions.md ADR-017): fanned out to every installed "fleet" module
// (e.g. teleport), a silent no-op if none is present (fleet.agent/v1 is an
// optional requires — Unimplemented is then the broker's normal answer, not an
// error), the same method as modules/chrony and modules/powerdns. It returns true when an agent is enrolled.
func (m *vaultModule) installFleetAgents(ctx context.Context, target connTarget) (bool, error) {
	_, err := m.fleetAgentClient.Install(ctx, &fleetagentv1.InstallRequest{
		Target: &fleetagentv1.Target{Host: target.Host, Port: target.Port, User: target.User, SshPrivateKey: target.PrivateKey},
	})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return false, nil
		}
		return false, fmt.Errorf("fleet.agent/v1.Install: %w", err)
	}
	return true, nil
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
		return sshKeyPair{}, fmt.Errorf("generating the SSH pair: %w", err)
	}
	resp, err := m.secretsClient.Get(ctx, &secretsv1.GetRequest{Ref: ref})
	if err != nil {
		return sshKeyPair{}, fmt.Errorf("reading the SSH pair: %w", err)
	}
	var pair sshKeyPair
	if err := json.Unmarshal([]byte(resp.GetValue()), &pair); err != nil {
		return sshKeyPair{}, fmt.Errorf("decoding the SSH pair: %w", err)
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
	Host           string
	Port           int32
	User           string
	PrivateKey     string
	CertificatePem string
}

func (t connTarget) ansible() *ansiblev1.Target {
	return &ansiblev1.Target{
		Host:              t.Host,
		Port:              t.Port,
		User:              t.User,
		SshPrivateKey:     t.PrivateKey,
		SshCertificatePem: t.CertificatePem,
	}
}

func (t connTarget) osBase() *osbasev1.Target {
	return &osbasev1.Target{Host: t.Host, Port: t.Port, User: t.User, SshPrivateKey: t.PrivateKey}
}

func (m *vaultModule) targetFromState(ctx context.Context, req *modulev1.StepRequest, pair sshKeyPair) (connTarget, error) {
	state := sdk.StateMap(req.GetState())
	if state["admin_method"] == "teleport" {
		certResp, err := m.accessSSHClient.SignUserKey(ctx, &accesssshv1.SignUserKeyRequest{
			Principals: []string{sshUser},
		})
		if err != nil {
			return connTarget{}, fmt.Errorf("signing SSH user certificate: %w", err)
		}
		return connTarget{
			Host:           fmt.Sprint(state["vm_ip"]),
			Port:           teleportAgentPort,
			User:           sshUser,
			PrivateKey:     certResp.GetPrivateKeyOpenssh(),
			CertificatePem: certResp.GetCertificateOpenssh(),
		}, nil
	}
	var port int64
	switch v := state["vm_ssh_port"].(type) {
	case int64:
		port = v
	case float64:
		port = int64(v)
	}
	return connTarget{Host: fmt.Sprint(state["vm_ip"]), Port: int32(port), User: sshUser, PrivateKey: pair.PrivateKeyOpenSSH}, nil
}

// Provision creates (or finds again, EnsureVM is idempotent) vault's dedicated
// VM.
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
		return nil, fmt.Errorf("EnsureVM(%q): %w", name, err)
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
		return nil, fmt.Errorf("DeleteVM(%q): %w", name, err)
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
