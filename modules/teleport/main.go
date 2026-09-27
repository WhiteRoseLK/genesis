// SPDX-License-Identifier: Apache-2.0

// teleport fournit access.ssh/v1 et fleet.agent/v1 en phase cible
// (docs/07-mvp-modules.md) : Teleport Community (Auth + Proxy) sur sa
// propre VM, avec sa propre CA interne pour les certificats SSH (pas de
// délégation à pki.issuer/v1 pour la signature SSH, docs/09-decisions.md
// ADR-018). fleet.agent/v1 est une fonction « de parc » (ADR-017) : chaque
// module qui provisionne une VM (chrony, powerdns, vault…) l'appelle depuis
// Configure pour installer l'agent Teleport -- c'est ce qui rend Teleport
// actif sur tout le parc dès sa présence dans la spec, sans qu'aucun module
// existant ne le connaisse spécifiquement.
//
// Portée assumée pour ce jalon : fleet.agent/v1.Install installe et enrôle
// l'agent mais NE désactive PAS le sshd natif -- le cœur ne bascule pas
// encore ses runners core.ansible/v1 vers l'agent (Repoint différé,
// décision explicite, docs/PROGRESS.md), ce qui casserait tout appel
// ultérieur de core.ansible/v1 sur la VM concernée (ex. Handover de vault).
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

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
	verifyCertTTL      = "5m"
	authPort           = 3025
	leafTTLSeconds     = int64(90 * 24 * 60 * 60) // 90 jours, même choix que modules/vault
	joinTokenTTL       = "10m"
)

// sshKeyPair reflète la valeur JSON produite par le générateur
// GENERATOR_SSH_KEYPAIR côté cœur (internal/secrets.SSHKeyPair) -- les
// modules n'important jamais internal/, la forme est dupliquée ici par son
// seul contrat, même choix que modules/vault et modules/powerdns.
type sshKeyPair struct {
	PrivateKeyOpenSSH   string `json:"private_key_openssh"`
	PublicKeyAuthorized string `json:"public_key_authorized"`
}

// connTarget porte les coordonnées de connexion à une VM cible, indépendant
// du type protobuf de la fonction qui les consomme.
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

	// Actif une fois Configure passé (même dette qu'ailleurs, en mémoire
	// seulement, docs/PROGRESS.md) : requis par fleet.agent/v1.Install et
	// access.ssh/v1.JumpHost, qui n'ont pas accès à req.State (appelés hors
	// du cycle Provision/Configure/Verify du cœur).
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

func (m *teleportModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	m.brokerToken = req.GetBrokerToken()
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_CONFORME}, nil
}

// dial dial la session de broker au plus une fois -- même précaution que
// modules/vault et modules/powerdns.
func (m *teleportModule) dial() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmClient != nil {
		return nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return fmt.Errorf("teleport : aucune session de broker (Check n'a pas encore été appelé)")
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
	m.pkiClient = pkiissuerv1.NewPkiIssuerClient(conn)
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

func clusterNameFromConfig(req *modulev1.StepRequest) string {
	cfg := sdk.StateMap(req.GetConfig())
	if n, ok := cfg["cluster_name"].(string); ok && n != "" {
		return n
	}
	return defaultClusterName
}

func (m *teleportModule) sshKeyPair(ctx context.Context, name string) (sshKeyPair, error) {
	ref := fmt.Sprintf(sshKeyRefFmt, name)
	if _, err := m.secretsClient.Ensure(ctx, &secretsv1.EnsureRequest{
		Ref: ref, Generator: secretsv1.Generator_GENERATOR_SSH_KEYPAIR,
		Meta: &secretsv1.Meta{Owner: "teleport", Consumers: []string{"teleport"}, Kind: "ssh_keypair"},
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

// Provision crée (ou retrouve, EnsureVM est idempotent) la VM dédiée
// Auth+Proxy de teleport.
func (m *teleportModule) Provision(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
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
	state["cluster_name"] = clusterNameFromConfig(req)
	s, err := sdk.NewState(state)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func (m *teleportModule) Destroy(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
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
