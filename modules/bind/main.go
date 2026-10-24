// SPDX-License-Identifier: Apache-2.0

// bind provides dns.zone/v1 and dns.resolver/v1 in the target phase
// (docs/10-adding-a-module.md): BIND 9 Authoritative + Recursive resolver on its
// own VM (compute.vm/v1); it takes over the zone from coredns at handover time
// (Handover reads dns.zone/v1@seed, recreates everything, updates zone files).
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	accesssshv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/access/ssh/v1"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	secretsv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/secrets/v1"
	dnsresolverv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/resolver/v1"
	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
	fleetagentv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/fleet/agent/v1"
	osbasev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

//go:embed playbooks/install_bind.yml
var installBindPlaybook []byte

//go:embed playbooks/update_zone.yml
var updateZonePlaybook []byte

//go:embed playbooks/check_resolution.yml
var checkResolutionPlaybook []byte

const (
	defaultVMName     = "bind01"
	defaultDomain     = "lab.internal"
	sshUser           = "genesis"
	sshKeyRefFmt      = "bind/%s/ssh-key"
	verifierSuffix    = "-verify"
	verifyProbeName   = "verify-probe"
	verifyProbeIP     = "10.255.255.1"
	teleportAgentPort = 3022
)

type sshKeyPair struct {
	PrivateKeyOpenSSH   string `json:"private_key_openssh"`
	PublicKeyAuthorized string `json:"public_key_authorized"`
}

type connTarget struct {
	Host           string
	Port           int32
	User           string
	PrivateKey     string
	CertificatePem string
	HostKey        string
}

func (t connTarget) ansible() *ansiblev1.Target {
	return &ansiblev1.Target{
		Host:              t.Host,
		Port:              t.Port,
		User:              t.User,
		SshPrivateKey:     t.PrivateKey,
		SshCertificatePem: t.CertificatePem,
		SshHostKey:        t.HostKey,
	}
}

func (t connTarget) osBase() *osbasev1.Target {
	return &osbasev1.Target{Host: t.Host, Port: t.Port, User: t.User, SshPrivateKey: t.PrivateKey, SshHostKey: t.HostKey}
}

type bindModule struct {
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
	dnsZoneSeedClient dnszonev1.DnsZoneClient
	fleetAgentClient  fleetagentv1.FleetAgentClient
	accessSSHClient   accesssshv1.AccessSSHClient

	zoneServer *bindZoneServer
}

func (m *bindModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *bindModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *bindModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func boolFlag(flags map[string]any, key string) bool {
	v, _ := flags[key].(bool)
	return v
}

func (m *bindModule) dialWithToken(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if token == "" {
		if m.vmClient != nil {
			return nil
		}
		return fmt.Errorf("bind: no broker session available")
	}
	if m.brokerToken == token && m.vmClient != nil {
		return nil
	}
	if m.broker == nil {
		return nil
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
	m.dnsZoneSeedClient = dnszonev1.NewDnsZoneClient(conn)
	m.fleetAgentClient = fleetagentv1.NewFleetAgentClient(conn)
	m.accessSSHClient = accesssshv1.NewAccessSSHClient(conn)
	return nil
}

func (m *bindModule) restoreZoneServer(flags map[string]any) {
	m.zoneServer.mu.Lock()
	defer m.zoneServer.mu.Unlock()

	if vmIP, ok := flags["vm_ip"].(string); ok && vmIP != "" {
		m.zoneServer.vmIP = vmIP
	}
	if dom, ok := flags["domain"].(string); ok && dom != "" {
		m.zoneServer.domain = dom
	}
	if boolFlag(flags, "configured") {
		m.zoneServer.configured = true
	}
	if rawRecords, ok := flags["records"].(map[string]any); ok {
		for k, v := range rawRecords {
			if recMap, ok := v.(map[string]any); ok {
				recBytes, err := json.Marshal(recMap)
				if err == nil {
					var rec dnszonev1.Record
					if err := json.Unmarshal(recBytes, &rec); err == nil {
						m.zoneServer.records[k] = &rec
					}
				}
			}
		}
	}
}

func (m *bindModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	_ = m.dialWithToken(req.GetBrokerToken())
	flags := sdk.StateMap(req.GetState())
	if boolFlag(flags, "configured") || boolFlag(flags, "handed_over") || boolFlag(flags, "verified") {
		m.restoreZoneServer(flags)
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_TODO}, nil
}

func (m *bindModule) SeedUp(context.Context, *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return nil, status.Errorf(codes.Unimplemented, "bind does not participate in the seed phase")
}

func (m *bindModule) SeedDown(context.Context, *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return nil, status.Errorf(codes.Unimplemented, "bind does not participate in the seed phase")
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

func domain(req *modulev1.StepRequest) string {
	cfg := sdk.StateMap(req.GetConfig())
	if d, ok := cfg["domain"].(string); ok && d != "" {
		return d
	}
	return defaultDomain
}

func verifierVMName(req *modulev1.StepRequest) (string, bool) {
	cfg := sdk.StateMap(req.GetConfig())
	if v, ok := cfg["verifier_vm"].(string); ok && v != "" {
		return v, true
	}
	return vmName(req) + verifierSuffix, false
}

func (m *bindModule) verifierSSHKeyPair(ctx context.Context, verifierName string, shared bool) (sshKeyPair, error) {
	if shared {
		resp, err := m.secretsClient.Get(ctx, &secretsv1.GetRequest{Ref: "verifier/ssh-key"})
		if err == nil {
			var pair sshKeyPair
			if err := json.Unmarshal([]byte(resp.GetValue()), &pair); err == nil && pair.PrivateKeyOpenSSH != "" {
				return pair, nil
			}
		}
	}
	return m.sshKeyPair(ctx, verifierName)
}

func (m *bindModule) sshKeyPair(ctx context.Context, name string) (sshKeyPair, error) {
	ref := fmt.Sprintf(sshKeyRefFmt, name)
	if _, err := m.secretsClient.Ensure(ctx, &secretsv1.EnsureRequest{
		Ref:       ref,
		Generator: secretsv1.Generator_GENERATOR_SSH_KEYPAIR,
		Meta:      &secretsv1.Meta{Owner: "bind", Consumers: []string{"bind"}, Kind: "ssh_keypair"},
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

func (m *bindModule) targetFromState(ctx context.Context, req *modulev1.StepRequest, pair sshKeyPair) (connTarget, error) {
	state := sdk.StateMap(req.GetState())
	var hostKey string
	if hk, ok := state["vm_host_key"].(string); ok {
		hostKey = hk
	}
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
			HostKey:        hostKey,
		}, nil
	}
	var port int64
	switch v := state["vm_ssh_port"].(type) {
	case int64:
		port = v
	case float64:
		port = int64(v)
	}
	return connTarget{
		Host:       fmt.Sprint(state["vm_ip"]),
		Port:       int32(port),
		User:       sshUser,
		PrivateKey: pair.PrivateKeyOpenSSH,
		HostKey:    hostKey,
	}, nil
}

func (m *bindModule) Provision(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetBrokerToken()); err != nil {
		return nil, err
	}
	name := vmName(req)
	dom := domain(req)

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
	if vm.GetSshHostPublicKey() != "" {
		state["vm_host_key"] = vm.GetSshHostPublicKey()
	}
	state["domain"] = dom

	m.zoneServer.mu.Lock()
	m.zoneServer.vmIP = vm.GetIp()
	m.zoneServer.domain = dom
	m.zoneServer.mu.Unlock()

	s, err := sdk.NewState(state)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func (m *bindModule) installFleetAgents(ctx context.Context, target connTarget) (bool, error) {
	if m.fleetAgentClient == nil {
		return false, nil
	}
	_, err := m.fleetAgentClient.Install(ctx, &fleetagentv1.InstallRequest{
		Target: &fleetagentv1.Target{
			Host:          target.Host,
			Port:          target.Port,
			User:          target.User,
			SshPrivateKey: target.PrivateKey,
			SshHostKey:    target.HostKey,
		},
	})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return false, nil
		}
		return false, fmt.Errorf("fleet.agent/v1.Install: %w", err)
	}
	return true, nil
}

func (m *bindModule) Configure(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetBrokerToken()); err != nil {
		return nil, err
	}
	name := vmName(req)
	dom := domain(req)
	pair, err := m.sshKeyPair(ctx, name)
	if err != nil {
		return nil, err
	}
	target, err := m.targetFromState(ctx, req, pair)
	if err != nil {
		return nil, err
	}

	ntpEndpoint, err := m.timeNTPClient.Endpoint(ctx, &timentpv1.Empty{})
	if err != nil {
		return nil, fmt.Errorf("reading time.ntp/v1: %w", err)
	}
	if _, err := m.osBaseClient.SetNTP(ctx, &osbasev1.SetNTPRequest{Target: target.osBase(), Servers: []string{ntpEndpoint.GetAddress()}}); err != nil {
		return nil, fmt.Errorf("SetNTP: %w", err)
	}

	resolverEndpoint, err := m.dnsResolverClient.Endpoint(ctx, &dnsresolverv1.Empty{})
	if err != nil {
		return nil, fmt.Errorf("reading dns.resolver/v1: %w", err)
	}
	if _, err := m.osBaseClient.SetResolver(ctx, &osbasev1.SetResolverRequest{Target: target.osBase(), Nameservers: []string{resolverEndpoint.GetAddress()}, Domain: dom}); err != nil {
		return nil, fmt.Errorf("SetResolver: %w", err)
	}

	m.zoneServer.mu.Lock()
	m.zoneServer.vmIP = target.Host
	m.zoneServer.domain = dom
	zoneContent := m.zoneServer.renderZoneFileLocked()
	m.zoneServer.mu.Unlock()

	vars, err := sdk.NewState(map[string]any{
		"domain":             dom,
		"upstream_resolvers": []any{"9.9.9.9", "1.1.1.1"},
		"zone_file_content":  zoneContent,
	})
	if err != nil {
		return nil, err
	}
	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target:       target.ansible(),
		PlaybookYaml: installBindPlaybook,
		Vars:         vars,
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetOk() {
		return nil, fmt.Errorf("Configure(bind) failed:\n%s", resp.GetOutput())
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
	modState["configured"] = true
	modState["vm_name"] = name
	modState["vm_ip"] = target.Host
	modState["domain"] = dom

	m.zoneServer.mu.Lock()
	m.zoneServer.configured = true
	modState["records"] = m.zoneServer.recordsStateLocked()
	m.zoneServer.mu.Unlock()

	s, err := sdk.NewState(modState)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func (m *bindModule) Handover(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetBrokerToken()); err != nil {
		return nil, err
	}
	dom := domain(req)
	seedZone, err := m.dnsZoneSeedClient.ListRecords(ctx, &dnszonev1.Zone{Zone: dom})
	if err != nil {
		return nil, fmt.Errorf("reading records from seed DNS: %w", err)
	}

	m.zoneServer.mu.Lock()
	for _, r := range seedZone.GetRecords() {
		m.zoneServer.records[recordKey(r.GetZone(), r.GetName(), r.GetType())] = r
	}
	zoneContent := m.zoneServer.renderZoneFileLocked()
	vmIP := m.zoneServer.vmIP
	m.zoneServer.mu.Unlock()

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
		"domain":            dom,
		"zone_file_content": zoneContent,
	})
	if err != nil {
		return nil, err
	}
	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target:       target.ansible(),
		PlaybookYaml: updateZonePlaybook,
		Vars:         vars,
	})
	if err != nil {
		return nil, fmt.Errorf("updating zone during handover: %w", err)
	}
	if !resp.GetOk() {
		return nil, fmt.Errorf("updating zone during handover failed:\n%s", resp.GetOutput())
	}

	modState := sdk.StateMap(req.GetState())
	modState["handed_over"] = true
	modState["vm_ip"] = vmIP
	modState["domain"] = dom

	m.zoneServer.mu.Lock()
	modState["records"] = m.zoneServer.recordsStateLocked()
	m.zoneServer.mu.Unlock()

	s, err := sdk.NewState(modState)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

var (
	forwardRe  = regexp.MustCompile(`"forward":\s*"([^"]*)"`)
	externalRe = regexp.MustCompile(`"external":\s*"([^"]*)"`)
)

func parseResolutionOutput(output string) (forward, external string, err error) {
	f := forwardRe.FindStringSubmatch(output)
	e := externalRe.FindStringSubmatch(output)
	if f == nil || e == nil {
		return "", "", fmt.Errorf("forward/external results not found in check_resolution output")
	}
	return f[1], e[1], nil
}

func (m *bindModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetBrokerToken()); err != nil {
		return nil, err
	}
	dom := domain(req)

	probeName := verifyProbeName
	probeIP := verifyProbeIP
	if _, err := m.zoneServer.UpsertRecord(ctx, &dnszonev1.Record{
		Zone: dom, Name: probeName, Type: "A", Values: []string{probeIP}, Ttl: 60,
	}); err != nil {
		return nil, fmt.Errorf("inserting probe record: %w", err)
	}

	verifierName, shared := verifierVMName(req)
	verifierPair, err := m.verifierSSHKeyPair(ctx, verifierName, shared)
	if err != nil {
		return nil, err
	}
	verifierVM, err := m.vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name: verifierName, Env: verifierName, SshPublicKey: verifierPair.PublicKeyAuthorized, User: sshUser,
	})
	if err != nil {
		return nil, fmt.Errorf("EnsureVM(%q) (verifier): %w", verifierName, err)
	}
	if !shared {
		defer func() {
			_, _ = m.vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: verifierName})
		}()
	}

	verifierTarget := connTarget{
		Host:       verifierVM.GetIp(),
		Port:       verifierVM.GetSshPort(),
		User:       sshUser,
		PrivateKey: verifierPair.PrivateKeyOpenSSH,
		HostKey:    verifierVM.GetSshHostPublicKey(),
	}

	m.zoneServer.mu.Lock()
	dnsServer := m.zoneServer.vmIP
	m.zoneServer.mu.Unlock()

	probeFQDN := probeName + "." + dom
	vars, err := sdk.NewState(map[string]any{
		"dns_server":    dnsServer,
		"forward_name":  probeFQDN,
		"external_name": "example.com",
	})
	if err != nil {
		return nil, err
	}
	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target:       verifierTarget.ansible(),
		PlaybookYaml: checkResolutionPlaybook,
		Vars:         vars,
	})
	if err != nil {
		return nil, fmt.Errorf("running check_resolution.yml: %w", err)
	}
	if !resp.GetOk() {
		return nil, fmt.Errorf("check_resolution.yml failed:\n%s", resp.GetOutput())
	}

	fwd, ext, err := parseResolutionOutput(resp.GetOutput())
	if err != nil {
		return nil, fmt.Errorf("parsing resolution output: %w\n%s", err, resp.GetOutput())
	}
	if fwd != probeIP {
		return nil, fmt.Errorf("Verify(bind): probe %q -> %q, want %q", probeFQDN, fwd, probeIP)
	}
	if ext == "" {
		return nil, fmt.Errorf("Verify(bind): empty resolution of external name")
	}

	state := sdk.StateMap(req.GetState())
	state["verified"] = true
	s, err := sdk.NewState(state)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func (m *bindModule) Repoint(ctx context.Context, req *modulev1.RepointRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetStep().GetBrokerToken()); err != nil {
		return nil, err
	}
	if req.GetFunction() == "fleet.agent/v1" {
		modState := sdk.StateMap(req.GetStep().GetState())
		if modState["admin_method"] != "teleport" {
			name := vmName(req.GetStep())
			pair, err := m.sshKeyPair(ctx, name)
			if err != nil {
				return nil, err
			}
			var port int64
			switch v := modState["vm_ssh_port"].(type) {
			case int64:
				port = v
			case float64:
				port = int64(v)
			}
			if port == 0 {
				port = 22
			}
			var hostKey string
			if hk, ok := modState["vm_host_key"].(string); ok {
				hostKey = hk
			}
			directTarget := connTarget{
				Host:       fmt.Sprint(modState["vm_ip"]),
				Port:       int32(port),
				User:       sshUser,
				PrivateKey: pair.PrivateKeyOpenSSH,
				HostKey:    hostKey,
			}
			enrolled, err := m.installFleetAgents(ctx, directTarget)
			if err != nil {
				return nil, err
			}
			if enrolled {
				modState["admin_method"] = "teleport"
				s, err := sdk.NewState(modState)
				if err != nil {
					return nil, err
				}
				return &modulev1.StepResult{
					Status: modulev1.StepResult_STATUS_OK,
					State:  s,
				}, nil
			}
		}
	}
	return &modulev1.StepResult{
		Status: modulev1.StepResult_STATUS_OK,
		State:  req.GetStep().GetState(),
	}, nil
}

func (m *bindModule) Destroy(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetBrokerToken()); err != nil {
		return nil, err
	}
	name := vmName(req)
	if _, err := m.vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: name}); err != nil {
		return nil, fmt.Errorf("DeleteVM(%q): %w", name, err)
	}

	m.zoneServer.mu.Lock()
	m.zoneServer.vmIP = ""
	m.zoneServer.domain = ""
	m.zoneServer.configured = false
	m.zoneServer.records = map[string]*dnszonev1.Record{}
	m.zoneServer.mu.Unlock()

	s, err := sdk.NewState(map[string]any{})
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

// --- dns.zone/v1 server ---------------------------------------------------

type bindZoneServer struct {
	dnszonev1.UnimplementedDnsZoneServer
	module *bindModule

	mu         sync.Mutex
	records    map[string]*dnszonev1.Record
	vmIP       string
	domain     string
	configured bool
}

func recordKey(zone, name, typ string) string {
	return zone + "|" + name + "|" + typ
}

func (s *bindZoneServer) renderZoneFileLocked() string {
	var b strings.Builder
	dom := s.domain
	if dom == "" {
		dom = defaultDomain
	}
	fmt.Fprintf(&b, "$ORIGIN %s.\n$TTL 300\n", dom)
	fmt.Fprintf(&b, "@ IN SOA ns1.%s. admin.%s. (%d 3600 900 604800 300)\n", dom, dom, time.Now().Unix())
	fmt.Fprintf(&b, "@ IN NS ns1.%s.\n", dom)
	if s.vmIP != "" {
		fmt.Fprintf(&b, "ns1 IN A %s\n", s.vmIP)
	}
	for _, r := range s.records {
		if r.GetZone() != dom {
			continue
		}
		name := r.GetName()
		if name == "" {
			name = "@"
		}
		for _, v := range r.GetValues() {
			fmt.Fprintf(&b, "%s IN %s %s\n", name, r.GetType(), v)
		}
	}
	return b.String()
}

func (s *bindZoneServer) recordsStateLocked() map[string]any {
	out := make(map[string]any, len(s.records))
	for k, r := range s.records {
		vals := make([]any, len(r.GetValues()))
		for i, v := range r.GetValues() {
			vals[i] = v
		}
		out[k] = map[string]any{
			"zone":   r.GetZone(),
			"name":   r.GetName(),
			"type":   r.GetType(),
			"values": vals,
			"ttl":    float64(r.GetTtl()),
		}
	}
	return out
}

func (s *bindZoneServer) UpsertRecord(_ context.Context, req *dnszonev1.Record) (*dnszonev1.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == nil {
		s.records = map[string]*dnszonev1.Record{}
	}
	if req.GetType() == "A" {
		if existing := s.records[recordKey(req.GetZone(), req.GetName(), req.GetType())]; existing != nil {
			for _, oldIP := range existing.GetValues() {
				if revKey, err := sdk.ReverseKeyForA(oldIP); err == nil {
					delete(s.records, recordKey(revKey.GetZone(), revKey.GetName(), revKey.GetType()))
				}
			}
		}
		fqdnStr := sdk.RecordFQDN(req.GetZone(), req.GetName())
		for _, ip := range req.GetValues() {
			if ptrRec, err := sdk.ReversePTRForA(ip, fqdnStr, req.GetTtl()); err == nil {
				s.records[recordKey(ptrRec.GetZone(), ptrRec.GetName(), ptrRec.GetType())] = ptrRec
			}
		}
	}
	s.records[recordKey(req.GetZone(), req.GetName(), req.GetType())] = req
	return &dnszonev1.Empty{}, nil
}

func (s *bindZoneServer) DeleteRecord(_ context.Context, req *dnszonev1.RecordKey) (*dnszonev1.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.GetType() == "A" {
		if existing := s.records[recordKey(req.GetZone(), req.GetName(), req.GetType())]; existing != nil {
			for _, ip := range existing.GetValues() {
				if revKey, err := sdk.ReverseKeyForA(ip); err == nil {
					delete(s.records, recordKey(revKey.GetZone(), revKey.GetName(), revKey.GetType()))
				}
			}
		}
	}
	delete(s.records, recordKey(req.GetZone(), req.GetName(), req.GetType()))
	return &dnszonev1.Empty{}, nil
}

func (s *bindZoneServer) ListRecords(_ context.Context, req *dnszonev1.Zone) (*dnszonev1.Records, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*dnszonev1.Record
	for _, r := range s.records {
		if r.GetZone() == req.GetZone() {
			out = append(out, r)
		}
	}
	return &dnszonev1.Records{Records: out}, nil
}

func (s *bindZoneServer) Endpoint(context.Context, *dnszonev1.Empty) (*dnszonev1.EndpointInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &dnszonev1.EndpointInfo{Address: s.vmIP, Port: 53}, nil
}

// --- dns.resolver/v1 adapter ----------------------------------------------

type resolverAdapter struct {
	dnsresolverv1.UnimplementedDnsResolverServer
	*bindZoneServer
}

func (r resolverAdapter) Endpoint(context.Context, *dnsresolverv1.Empty) (*dnsresolverv1.EndpointInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &dnsresolverv1.EndpointInfo{Address: r.vmIP, Port: 53}, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	module := &bindModule{manifest: mf.ToProto()}
	zoneServer := &bindZoneServer{module: module, records: map[string]*dnszonev1.Record{}}
	module.zoneServer = zoneServer

	sdk.Serve(module,
		sdk.FunctionProvider{
			Name:     "dns.zone/v1",
			Register: func(s *grpc.Server) { dnszonev1.RegisterDnsZoneServer(s, zoneServer) },
		},
		sdk.FunctionProvider{
			Name: "dns.resolver/v1",
			Register: func(s *grpc.Server) {
				dnsresolverv1.RegisterDnsResolverServer(s, resolverAdapter{bindZoneServer: zoneServer})
			},
		},
	)
}
