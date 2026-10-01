// SPDX-License-Identifier: Apache-2.0

// teleport provides access.ssh/v1 and fleet.agent/v1 in the target phase
// (docs/07-mvp-modules.md): Teleport Community (Auth + Proxy) on its own VM,
// with its own internal CA for SSH certificates (no delegation to
// pki.issuer/v1 for SSH signing, docs/09-decisions.md ADR-018). fleet.agent/v1
// is a "fleet" function (ADR-017): every module that provisions a VM (chrony,
// powerdns, vault…) calls it from Configure to install the Teleport agent --
// this is what makes Teleport active across the whole fleet as soon as it is
// in the spec, without any existing module knowing about it specifically.
// fleet.agent/v1.Install installs and enrols the agent, and disables the
// native sshd once confirmed running -- callers switch to the Teleport agent
// on port 3022 with an OpenSSH user certificate (docs/09-decisions.md ADR-018).
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	accesssshv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/access/ssh/v1"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	secretsv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/secrets/v1"
	dnsresolverv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/resolver/v1"
	fleetagentv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/fleet/agent/v1"
	osbasev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	pkiissuerv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/pki/issuer/v1"
	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

//go:embed playbooks/install_teleport.yml
var installTeleportPlaybook []byte

//go:embed playbooks/new_join_token.yml
var newJoinTokenPlaybook []byte

//go:embed playbooks/install_agent.yml
var installAgentPlaybook []byte

//go:embed playbooks/generate_user_cert.yml
var generateUserCertPlaybook []byte

//go:embed playbooks/check_ssh.yml
var checkSSHPlaybook []byte

const (
	defaultVMName      = "teleport01"
	defaultClusterName = "genesis"
	sshUser            = "genesis"
	sshKeyRefFmt       = "teleport/%s/ssh-key"
	agentTargetSuffix  = "-agent-target"
	verifierSuffix     = "-verify"
	verifyTeleportUser = "genesis-verify"
	verifyCertTTL      = 5 * time.Minute
	authPort           = 3025
	leafTTLSeconds     = int64(90 * 24 * 60 * 60) // 90 days, the same choice as modules/vault
	joinTokenTTL       = "10m"
)

// sshKeyPair mirrors the JSON value produced by the core's
// GENERATOR_SSH_KEYPAIR generator (internal/secrets.SSHKeyPair) -- since
// modules never import internal/, the shape is duplicated here from its
// contract alone, the same choice as modules/vault and modules/powerdns.
type sshKeyPair struct {
	PrivateKeyOpenSSH   string `json:"private_key_openssh"`
	PublicKeyAuthorized string `json:"public_key_authorized"`
}

// connTarget holds the connection details of a target VM, independently of the
// protobuf type of the function that consumes them.
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

type teleportModule struct {
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
	pkiClient         pkiissuerv1.PkiIssuerClient

	// Active once Configure has passed (the same debt as elsewhere, in memory
	// only, docs/PROGRESS.md): required by fleet.agent/v1.Install and
	// access.ssh/v1.JumpHost, which have no access to req.State (called
	// outside the core's Provision/Configure/Verify cycle).
	ownTarget   connTarget
	clusterName string
	caPin       string
}

func (m *teleportModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *teleportModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *teleportModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *teleportModule) Check(ctx context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	if req.GetBrokerToken() != "" && m.broker != nil {
		_ = m.dialWithToken(req.GetBrokerToken())
	}
	state := sdk.StateMap(req.GetState())
	if pin, ok := state["ca_pin"].(string); ok && pin != "" {
		m.mu.Lock()
		m.caPin = pin
		if cn, ok := state["cluster_name"].(string); ok {
			m.clusterName = cn
		}
		m.mu.Unlock()
		name := vmName(req)
		if pair, err := m.sshKeyPair(ctx, name); err == nil {
			m.mu.Lock()
			m.ownTarget = targetFromState(req, pair)
			m.mu.Unlock()
		}
	}
	if !boolFlag(state, "verified") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_TODO}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
}

func boolFlag(flags map[string]any, key string) bool {
	v, _ := flags[key].(bool)
	return v
}

// dialWithToken dials the broker session with token, or the cached token if
// token is empty. If a new token is provided, it redials so that repoints are
// immediately observed (the test-b/test-f pattern, docs/PROGRESS.md debt #26).
func (m *teleportModule) dialWithToken(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if token == "" {
		token = m.brokerToken
	}
	if token == "" {
		if m.vmClient != nil {
			return nil
		}
		return fmt.Errorf("teleport: no broker session available")
	}
	if m.brokerToken == token && m.vmClient != nil {
		return nil
	}
	if m.broker == nil {
		return fmt.Errorf("teleport: broker client not set")
	}
	conn, err := m.broker.Dial(token)
	if err != nil {
		return fmt.Errorf("connecting to the required functions: %w", err)
	}
	m.brokerToken = token
	m.vmClient = computevmv1.NewComputeVMClient(conn)
	m.osBaseClient = osbasev1.NewBaseClient(conn)
	m.ansibleClient = ansiblev1.NewAnsibleClient(conn)
	m.secretsClient = secretsv1.NewSecretsClient(conn)
	m.timeNTPClient = timentpv1.NewTimeNTPClient(conn)
	m.dnsResolverClient = dnsresolverv1.NewDnsResolverClient(conn)
	m.pkiClient = pkiissuerv1.NewPkiIssuerClient(conn)
	return nil
}

func (m *teleportModule) dial() error {
	return m.dialWithToken("")
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

func clusterNameFromConfig(req *modulev1.StepRequest) string {
	cfg := sdk.StateMap(req.GetConfig())
	if n, ok := cfg["cluster_name"].(string); ok && n != "" {
		return n
	}
	return defaultClusterName
}

func (m *teleportModule) sshKeyPair(ctx context.Context, name string) (sshKeyPair, error) {
	if m.secretsClient == nil {
		return sshKeyPair{}, fmt.Errorf("secrets client not connected")
	}
	ref := fmt.Sprintf(sshKeyRefFmt, name)
	if _, err := m.secretsClient.Ensure(ctx, &secretsv1.EnsureRequest{
		Ref: ref, Generator: secretsv1.Generator_GENERATOR_SSH_KEYPAIR,
		Meta: &secretsv1.Meta{Owner: "teleport", Consumers: []string{"teleport"}, Kind: "ssh_keypair"},
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

func targetFromState(req *modulev1.StepRequest, pair sshKeyPair) connTarget {
	state := sdk.StateMap(req.GetState())
	host := fmt.Sprint(state["vm_ip"])
	if h, ok := state["own_target_host"].(string); ok && h != "" {
		host = h
	}
	user := sshUser
	if u, ok := state["own_target_user"].(string); ok && u != "" {
		user = u
	}
	var port int64
	portVal := state["own_target_port"]
	if portVal == nil {
		portVal = state["vm_ssh_port"]
	}
	switch v := portVal.(type) {
	case int64:
		port = v
	case float64:
		port = int64(v)
	case int:
		port = int64(v)
	case int32:
		port = int64(v)
	}
	return connTarget{Host: host, Port: int32(port), User: user, PrivateKey: pair.PrivateKeyOpenSSH}
}

// Provision creates (or finds again, EnsureVM is idempotent) teleport's
// dedicated Auth+Proxy VM.
func (m *teleportModule) Provision(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetBrokerToken()); err != nil {
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
	state["cluster_name"] = clusterNameFromConfig(req)
	s, err := sdk.NewState(state)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func (m *teleportModule) Destroy(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetBrokerToken()); err != nil {
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
	module := &teleportModule{manifest: mf.ToProto()}
	fleetServer := &teleportFleetServer{module: module}
	accessServer := &teleportAccessServer{module: module}
	sdk.Serve(module,
		sdk.FunctionProvider{
			Name:     "fleet.agent/v1",
			Register: func(s *grpc.Server) { fleetagentv1.RegisterFleetAgentServer(s, fleetServer) },
		},
		sdk.FunctionProvider{
			Name:     "access.ssh/v1",
			Register: func(s *grpc.Server) { accesssshv1.RegisterAccessSSHServer(s, accessServer) },
		},
	)
}
