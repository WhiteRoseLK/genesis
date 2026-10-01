// SPDX-License-Identifier: Apache-2.0

// chrony provides time.ntp/v1 in the target phase (docs/07-mvp-modules.md):
// unlike base-os or fake-compute, chrony owns its own VM (compute.vm/v1) and
// installs/configures it itself as a chronyd server (core.ansible/v1) —
// os.base/v1 stays declared in requires (ADR-016) but is not called yet:
// neither CA nor resolver is orchestrated at this stage (step-ca/vault arrive
// in M7), noted as debt in docs/PROGRESS.md.
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
	accesssshv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/access/ssh/v1"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	secretsv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/secrets/v1"
	fleetagentv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/fleet/agent/v1"
	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// sshKeyPair mirrors the JSON value produced by the core's
// GENERATOR_SSH_KEYPAIR generator (internal/secrets.SSHKeyPair) — since
// modules never import internal/, the shape is duplicated here from its
// contract alone (two JSON fields), not by import.
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
	defaultVMName     = "chrony01"
	sshUser           = "genesis"
	sshKeyRefFmt      = "chrony/%s/ssh-key"
	verifierSuffix    = "-verify"
	maxOffsetMs       = 100.0
	teleportAgentPort = 3022
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
	accessSSHClient  accesssshv1.AccessSSHClient
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
	flags := sdk.StateMap(req.GetState())
	if !boolFlag(flags, "verified") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_TODO}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
}

func boolFlag(flags map[string]any, key string) bool {
	v, _ := flags[key].(bool)
	return v
}

// dial dials the broker session at most once (Dial only succeeds once per
// token) and builds the three typed clients on the same gRPC connection — the
// same precaution as modules/base-os and modules/fake-compute.
func (m *chronyModule) dial() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmClient != nil {
		return nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return fmt.Errorf("chrony: no broker session (Check has not been called yet)")
	}
	conn, err := m.broker.Dial(m.brokerToken)
	if err != nil {
		return fmt.Errorf("connecting to the required functions: %w", err)
	}
	m.vmClient = computevmv1.NewComputeVMClient(conn)
	m.ansibleClient = ansiblev1.NewAnsibleClient(conn)
	m.secretsClient = secretsv1.NewSecretsClient(conn)
	m.fleetAgentClient = fleetagentv1.NewFleetAgentClient(conn)
	m.accessSSHClient = accesssshv1.NewAccessSSHClient(conn)
	return nil
}

// installFleetAgents calls fleet.agent/v1.Install(target)
// (docs/09-decisions.md ADR-017): fanned out to every installed "fleet" module
// (e.g. teleport), a silent no-op if none is present (fleet.agent/v1 is an
// optional requires — Unimplemented is then the broker's normal answer, not an
// error). It returns true when an agent is enrolled.
func (m *chronyModule) installFleetAgents(ctx context.Context, target *ansiblev1.Target) (bool, error) {
	_, err := m.fleetAgentClient.Install(ctx, &fleetagentv1.InstallRequest{
		Target: &fleetagentv1.Target{Host: target.GetHost(), Port: target.GetPort(), User: target.GetUser(), SshPrivateKey: target.GetSshPrivateKey()},
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

// sshKeyPair generates (idempotently, through core.secrets/v1) then fetches
// the service SSH pair of the chrony VM — the public key is injected through
// cloud-init, the private key is reused for the ansible connections.
func (m *chronyModule) sshKeyPair(ctx context.Context, name string) (sshKeyPair, error) {
	ref := fmt.Sprintf(sshKeyRefFmt, name)
	if _, err := m.secretsClient.Ensure(ctx, &secretsv1.EnsureRequest{
		Ref:       ref,
		Generator: secretsv1.Generator_GENERATOR_SSH_KEYPAIR,
		Meta:      &secretsv1.Meta{Owner: "chrony", Consumers: []string{"chrony"}, Kind: "ssh_keypair"},
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

// Provision creates (or finds again, EnsureVM is idempotent) chrony's
// dedicated VM and keeps its name and SSH key reference in the state.
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

func (m *chronyModule) targetFromState(ctx context.Context, req *modulev1.StepRequest, pair sshKeyPair) (*ansiblev1.Target, error) {
	state := sdk.StateMap(req.GetState())
	if state["admin_method"] == "teleport" {
		certResp, err := m.accessSSHClient.SignUserKey(ctx, &accesssshv1.SignUserKeyRequest{
			Principals: []string{sshUser},
		})
		if err != nil {
			return nil, fmt.Errorf("signing SSH user certificate: %w", err)
		}
		return &ansiblev1.Target{
			Host:              fmt.Sprint(state["vm_ip"]),
			Port:              teleportAgentPort,
			User:              sshUser,
			SshPrivateKey:     certResp.GetPrivateKeyOpenssh(),
			SshCertificatePem: certResp.GetCertificateOpenssh(),
		}, nil
	}
	port, _ := state["vm_ssh_port"].(int64)
	if port == 0 {
		if f, ok := state["vm_ssh_port"].(float64); ok {
			port = int64(f)
		}
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("configure: invalid SSH port %d in the module state (expected 1-65535), run provision again", port)
	}
	return &ansiblev1.Target{
		Host:          fmt.Sprint(state["vm_ip"]),
		Port:          int32(port),
		User:          sshUser,
		SshPrivateKey: pair.PrivateKeyOpenSSH,
	}, nil
}

// Configure installs and configures chronyd as a server on chrony's VM
// (docs/07-mvp-modules.md).
func (m *chronyModule) Configure(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	pair, err := m.sshKeyPair(ctx, name)
	if err != nil {
		return nil, err
	}
	target, err := m.targetFromState(ctx, req, pair)
	if err != nil {
		return nil, err
	}

	vars, err := sdk.NewState(map[string]any{
		"ntp_pools":  toAnySlice(ntpPools(req)),
		"allow_cidr": "0.0.0.0/0", // MVP: no network CIDR propagated yet (debt, docs/PROGRESS.md)
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
		return nil, fmt.Errorf("Configure(chrony) failed:\n%s", resp.GetOutput())
	}
	modState := sdk.StateMap(req.GetState())
	if modState["admin_method"] != "teleport" {
		enrolled, err := m.installFleetAgents(ctx, target)
		if err != nil {
			return nil, err
		}
		if enrolled {
			modState["admin_method"] = "teleport"
		}
	}

	m.mu.Lock()
	m.ntpEndpoint = &timentpv1.EndpointInfo{Address: target.GetHost(), Port: 123}
	m.mu.Unlock()

	s, err := sdk.NewState(modState)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

var systemTimeRe = regexp.MustCompile(`System time\s*:\s*([-\d.]+) seconds`)

// Verify proves a real synchronisation from a disposable third-party VM
// pointed at the chrony server (docs/03-module-contract.md rule 2: a test from
// a consumer's point of view, not the state of a process).
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
		return nil, fmt.Errorf("EnsureVM(%q) (verifier): %w", verifierName, err)
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
		return nil, fmt.Errorf("Verify(chrony) failed:\n%s", resp.GetOutput())
	}

	offsetMs, err := parseOffsetMs(resp.GetOutput())
	if err != nil {
		return nil, fmt.Errorf("Verify(chrony): %w\noutput:\n%s", err, resp.GetOutput())
	}
	if offsetMs >= maxOffsetMs {
		return nil, fmt.Errorf("Verify(chrony): offset of %.3f ms >= %.0f ms", offsetMs, maxOffsetMs)
	}

	state := sdk.StateMap(req.GetState())
	state["verified"] = true
	s, err := sdk.NewState(state)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

// parseOffsetMs extracts the "System time" line from the output of `chronyc
// tracking` (doc 07: offset < 100 ms).
func parseOffsetMs(output string) (float64, error) {
	matches := systemTimeRe.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("\"System time\" line not found in the output of chronyc tracking")
	}
	last := matches[len(matches)-1]
	seconds, err := strconv.ParseFloat(last[1], 64)
	if err != nil {
		return 0, fmt.Errorf("unreadable offset value %q: %w", last[1], err)
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
		return nil, fmt.Errorf("DeleteVM(%q): %w", name, err)
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

// timeNTPServer implements time.ntp/v1: the access point of the chrony server
// configured by Configure.
type timeNTPServer struct {
	timentpv1.UnimplementedTimeNTPServer
	module *chronyModule
}

func (s *timeNTPServer) Endpoint(context.Context, *timentpv1.Empty) (*timentpv1.EndpointInfo, error) {
	s.module.mu.Lock()
	defer s.module.mu.Unlock()
	if s.module.ntpEndpoint == nil {
		return nil, fmt.Errorf("time.ntp/v1: chrony not configured yet (Configure has not succeeded yet)")
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
