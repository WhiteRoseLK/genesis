// SPDX-License-Identifier: Apache-2.0

// chrony fournit time.ntp/v1 en phase cible (docs/07-modules-mvp.md) :
// contrairement à base-os ou fake-compute, chrony possède sa propre VM
// (compute.vm/v1) et l'installe/configure lui-même en serveur chronyd
// (core.ansible/v1) — os.base/v1 reste déclaré en requires (ADR-016) mais
// n'est pas encore appelé : ni CA ni résolveur ne sont orchestrés à ce
// stade (step-ca/vault arrivent en J7), noté en dette dans docs/PROGRESS.md.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	secretsv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/secrets/v1"
	fleetagentv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/fleet/agent/v1"
	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// sshKeyPair reflète la valeur JSON produite par le générateur
// GENERATOR_SSH_KEYPAIR côté cœur (internal/secrets.SSHKeyPair) — les
// modules n'important jamais internal/, la forme est dupliquée ici par son
// seul contrat (deux champs JSON), pas par import.
type sshKeyPair struct {
	PrivateKeyOpenSSH   string `json:"private_key_openssh"`
	PublicKeyAuthorized string `json:"public_key_authorized"`
}

//go:embed module.yaml
var manifestYAML []byte

//go:embed playbooks/install_chrony.yml
var installChronyPlaybook []byte

//go:embed playbooks/check_offset.yml
var checkOffsetPlaybook []byte

const (
	defaultVMName  = "chrony01"
	sshUser        = "genesis"
	sshKeyRefFmt   = "chrony/%s/ssh-key"
	verifierSuffix = "-verify"
	maxOffsetMs    = 100.0
)

type chronyModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest

	mu               sync.Mutex
	broker           *sdk.BrokerClient
	brokerToken      string
	vmClient         computevmv1.ComputeVMClient
	ansibleClient    ansiblev1.AnsibleClient
	secretsClient    secretsv1.SecretsClient
	fleetAgentClient fleetagentv1.FleetAgentClient
	ntpEndpoint      *timentpv1.EndpointInfo
}

func (m *chronyModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *chronyModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *chronyModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *chronyModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	m.brokerToken = req.GetBrokerToken()
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_CONFORME}, nil
}

// dial dial la session de broker au plus une fois (Dial ne réussit qu'une
// fois par jeton) et construit les trois clients typés sur la même
// connexion gRPC — même précaution que modules/base-os et modules/fake-compute.
func (m *chronyModule) dial() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmClient != nil {
		return nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return fmt.Errorf("chrony : aucune session de broker (Check n'a pas encore été appelé)")
	}
	conn, err := m.broker.Dial(m.brokerToken)
	if err != nil {
		return fmt.Errorf("connexion aux fonctions requises : %w", err)
	}
	m.vmClient = computevmv1.NewComputeVMClient(conn)
	m.ansibleClient = ansiblev1.NewAnsibleClient(conn)
	m.secretsClient = secretsv1.NewSecretsClient(conn)
	m.fleetAgentClient = fleetagentv1.NewFleetAgentClient(conn)
	return nil
}

// installFleetAgents appelle fleet.agent/v1.Install(target) (docs/09-decisions.md
// ADR-017) : diffusé vers tout module « de parc » installé (ex. teleport),
// no-op silencieux si aucun n'est présent (fleet.agent/v1 est un requires
// optionnel — Unimplemented est alors la réponse normale du broker, pas
// une erreur).
func (m *chronyModule) installFleetAgents(ctx context.Context, target *ansiblev1.Target) error {
	_, err := m.fleetAgentClient.Install(ctx, &fleetagentv1.InstallRequest{
		Target: &fleetagentv1.Target{Host: target.GetHost(), Port: target.GetPort(), User: target.GetUser(), SshPrivateKey: target.GetSshPrivateKey()},
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

func ntpPools(req *modulev1.StepRequest) []string {
	cfg := sdk.StateMap(req.GetConfig())
	raw, ok := cfg["ntp_pools"].([]any)
	if !ok {
		return []string{"pool.ntp.org"}
	}
	pools := make([]string, 0, len(raw))
	for _, p := range raw {
		if s, ok := p.(string); ok {
			pools = append(pools, s)
		}
	}
	return pools
}

// sshKeyPair génère (idempotent, via core.secrets/v1) puis récupère la paire
// SSH de service de la VM chrony — clé publique injectée en cloud-init,
// clé privée réutilisée pour les connexions ansible.
func (m *chronyModule) sshKeyPair(ctx context.Context, name string) (sshKeyPair, error) {
	ref := fmt.Sprintf(sshKeyRefFmt, name)
	if _, err := m.secretsClient.Ensure(ctx, &secretsv1.EnsureRequest{
		Ref:       ref,
		Generator: secretsv1.Generator_GENERATOR_SSH_KEYPAIR,
		Meta:      &secretsv1.Meta{Owner: "chrony", Consumers: []string{"chrony"}, Kind: "ssh_keypair"},
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

// Provision crée (ou retrouve, EnsureVM est idempotent) la VM dédiée de
// chrony et conserve son nom et sa référence de clé SSH dans l'état.
func (m *chronyModule) Provision(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)

	pair, err := m.sshKeyPair(ctx, name)
	if err != nil {
		return nil, err
	}

	vm, err := m.vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name:         name,
		Env:          name,
		SshPublicKey: pair.PublicKeyAuthorized,
		User:         sshUser,
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

func targetFromState(req *modulev1.StepRequest, pair sshKeyPair) (*ansiblev1.Target, error) {
	state := sdk.StateMap(req.GetState())
	port, _ := state["vm_ssh_port"].(int64)
	if port == 0 {
		if f, ok := state["vm_ssh_port"].(float64); ok {
			port = int64(f)
		}
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("configure : port SSH %d invalide dans l'état du module (attendu 1-65535), relancer provision", port)
	}
	return &ansiblev1.Target{
		Host:          fmt.Sprint(state["vm_ip"]),
		Port:          int32(port),
		User:          sshUser,
		SshPrivateKey: pair.PrivateKeyOpenSSH,
	}, nil
}

// Configure installe et configure chronyd en serveur sur la VM de chrony
// (docs/07-modules-mvp.md).
func (m *chronyModule) Configure(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	pair, err := m.sshKeyPair(ctx, name)
	if err != nil {
		return nil, err
	}
	target, err := targetFromState(req, pair)
	if err != nil {
		return nil, err
	}

	vars, err := sdk.NewState(map[string]any{
		"ntp_pools":  toAnySlice(ntpPools(req)),
		"allow_cidr": "0.0.0.0/0", // MVP : pas encore de CIDR réseau propagé (dette, docs/PROGRESS.md)
	})
	if err != nil {
		return nil, err
	}
	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target:       target,
		PlaybookYaml: installChronyPlaybook,
		Vars:         vars,
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetOk() {
		return nil, fmt.Errorf("Configure(chrony) a échoué :\n%s", resp.GetOutput())
	}
	if err := m.installFleetAgents(ctx, target); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.ntpEndpoint = &timentpv1.EndpointInfo{Address: target.GetHost(), Port: 123}
	m.mu.Unlock()

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

var systemTimeRe = regexp.MustCompile(`System time\s*:\s*([-\d.]+) seconds`)

// Verify prouve une synchronisation réelle depuis une VM tierce jetable
// pointée vers le serveur chrony (docs/03-contrat-module.md règle 2 : test
// depuis un point de vue consommateur, pas l'état d'un processus).
func (m *chronyModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	verifierName := name + verifierSuffix

	pair, err := m.sshKeyPair(ctx, verifierName)
	if err != nil {
		return nil, err
	}
	verifierVM, err := m.vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name:         verifierName,
		Env:          verifierName,
		SshPublicKey: pair.PublicKeyAuthorized,
		User:         sshUser,
	})
	if err != nil {
		return nil, fmt.Errorf("EnsureVM(%q) (vérificateur) : %w", verifierName, err)
	}
	defer func() {
		_, _ = m.vmClient.DeleteVM(context.Background(), &computevmv1.DeleteVMRequest{Name: verifierName})
	}()

	serverState := sdk.StateMap(req.GetState())
	target := &ansiblev1.Target{
		Host:          verifierVM.GetIp(),
		Port:          verifierVM.GetSshPort(),
		User:          sshUser,
		SshPrivateKey: pair.PrivateKeyOpenSSH,
	}
	vars, err := sdk.NewState(map[string]any{"ntp_server": fmt.Sprint(serverState["vm_ip"])})
	if err != nil {
		return nil, err
	}

	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target:       target,
		PlaybookYaml: checkOffsetPlaybook,
		Vars:         vars,
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetOk() {
		return nil, fmt.Errorf("Verify(chrony) a échoué :\n%s", resp.GetOutput())
	}

	offsetMs, err := parseOffsetMs(resp.GetOutput())
	if err != nil {
		return nil, fmt.Errorf("Verify(chrony) : %w\nsortie :\n%s", err, resp.GetOutput())
	}
	if offsetMs >= maxOffsetMs {
		return nil, fmt.Errorf("Verify(chrony) : écart de %.3f ms >= %.0f ms", offsetMs, maxOffsetMs)
	}

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

// parseOffsetMs extrait la ligne « System time » de la sortie de
// `chronyc tracking` (docs07 : écart < 100 ms).
func parseOffsetMs(output string) (float64, error) {
	matches := systemTimeRe.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("ligne « System time » introuvable dans la sortie de chronyc tracking")
	}
	last := matches[len(matches)-1]
	seconds, err := strconv.ParseFloat(last[1], 64)
	if err != nil {
		return 0, fmt.Errorf("valeur d'écart illisible %q : %w", last[1], err)
	}
	if seconds < 0 {
		seconds = -seconds
	}
	return seconds * 1000, nil
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func (m *chronyModule) Destroy(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	if _, err := m.vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: name}); err != nil {
		return nil, fmt.Errorf("DeleteVM(%q) : %w", name, err)
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

// timeNTPServer implémente time.ntp/v1 : point d'accès du serveur chrony
// configuré par Configure.
type timeNTPServer struct {
	timentpv1.UnimplementedTimeNTPServer
	module *chronyModule
}

func (s *timeNTPServer) Endpoint(context.Context, *timentpv1.Empty) (*timentpv1.EndpointInfo, error) {
	s.module.mu.Lock()
	defer s.module.mu.Unlock()
	if s.module.ntpEndpoint == nil {
		return nil, fmt.Errorf("time.ntp/v1 : chrony pas encore configuré (Configure n'a pas encore réussi)")
	}
	return s.module.ntpEndpoint, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	module := &chronyModule{manifest: mf.ToProto()}
	ntpServer := &timeNTPServer{module: module}
	sdk.Serve(module, sdk.FunctionProvider{
		Name:     "time.ntp/v1",
		Register: func(s *grpc.Server) { timentpv1.RegisterTimeNTPServer(s, ntpServer) },
	})
}
