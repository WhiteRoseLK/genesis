// SPDX-License-Identifier: Apache-2.0

// powerdns provides dns.zone/v1 and dns.resolver/v1 in the target phase
// (docs/07-mvp-modules.md): PowerDNS Authoritative (SQLite, API) + Recursor on
// its own VM (compute.vm/v1); it takes over the zone from coredns at handover
// time (Handover reads dns.zone/v1@seed, recreates everything, compares).
//
// Accepted scope for this milestone: like modules/coredns, the PowerDNS API
// connection parameters (address, key) live in memory in the module process,
// populated by Configure — a restart of the core mid-lifecycle would lose them
// (the same debt as coredns, docs/PROGRESS.md).
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
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
	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
	fleetagentv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/fleet/agent/v1"
	osbasev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

//go:embed playbooks/install_powerdns.yml
var installPowerDNSPlaybook []byte

//go:embed playbooks/check_resolution.yml
var checkResolutionPlaybook []byte

const (
	defaultVMName     = "powerdns01"
	defaultDomain     = "lab.internal"
	sshUser           = "genesis"
	sshKeyRefFmt      = "powerdns/%s/ssh-key"
	apiKeyRefFmt      = "powerdns/%s/api-key"
	verifierSuffix    = "-verify"
	pdnsAPIPort       = 8081
	verifyProbeName   = "verify-probe"
	verifyProbeIP     = "10.255.255.1"
	verifyReverseIP1  = "1"
	verifyReverseZ    = "255.255.10.in-addr.arpa"
	teleportAgentPort = 3022
)

// sshKeyPair mirrors the JSON value of the core's GENERATOR_SSH_KEYPAIR
// generator (internal/secrets.SSHKeyPair) — duplicated here from its JSON
// contract, since modules never import internal/ (the same choice as chrony).
type sshKeyPair struct {
	PrivateKeyOpenSSH   string `json:"private_key_openssh"`
	PublicKeyAuthorized string `json:"public_key_authorized"`
}

type powerdnsModule struct {
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

	zoneServer *powerdnsZoneServer
}

func (m *powerdnsModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *powerdnsModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *powerdnsModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *powerdnsModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
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

// dial dials the broker session at most once (Dial only succeeds once per
// token) and builds all the typed clients on the same connection — the same
// precaution as modules/chrony. dns.zone/v1@seed resolves to the active seed
// provider (coredns) through the registry's qualified key (internal/engine),
// dns.resolver/v1 and the others through their active key
// (docs/05-bootstrap-lifecycle.md).
func (m *powerdnsModule) dial() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmClient != nil {
		return nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return fmt.Errorf("powerdns: no broker session (Check has not been called yet)")
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
	m.dnsZoneSeedClient = dnszonev1.NewDnsZoneClient(conn)
	m.fleetAgentClient = fleetagentv1.NewFleetAgentClient(conn)
	m.accessSSHClient = accesssshv1.NewAccessSSHClient(conn)
	return nil
}

// installFleetAgents calls fleet.agent/v1.Install(target)
// (docs/09-decisions.md ADR-017): fanned out to every installed "fleet" module
// (e.g. teleport), a silent no-op if none is present (fleet.agent/v1 is an
// optional requires — Unimplemented is then the broker's normal answer, not an
// error), the same method as modules/chrony. It returns true when an agent is enrolled.
func (m *powerdnsModule) installFleetAgents(ctx context.Context, target connTarget) (bool, error) {
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

func domain(req *modulev1.StepRequest) string {
	cfg := sdk.StateMap(req.GetConfig())
	if d, ok := cfg["domain"].(string); ok && d != "" {
		return d
	}
	return defaultDomain
}

func (m *powerdnsModule) sshKeyPair(ctx context.Context, name string) (sshKeyPair, error) {
	ref := fmt.Sprintf(sshKeyRefFmt, name)
	if _, err := m.secretsClient.Ensure(ctx, &secretsv1.EnsureRequest{
		Ref:       ref,
		Generator: secretsv1.Generator_GENERATOR_SSH_KEYPAIR,
		Meta:      &secretsv1.Meta{Owner: "powerdns", Consumers: []string{"powerdns"}, Kind: "ssh_keypair"},
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

func (m *powerdnsModule) apiKeySecret(ctx context.Context, name string) (string, error) {
	ref := fmt.Sprintf(apiKeyRefFmt, name)
	if _, err := m.secretsClient.Ensure(ctx, &secretsv1.EnsureRequest{
		Ref:       ref,
		Generator: secretsv1.Generator_GENERATOR_TOKEN,
		Meta:      &secretsv1.Meta{Owner: "powerdns", Consumers: []string{"powerdns"}, Kind: "api-key"},
	}); err != nil {
		return "", fmt.Errorf("generating the API key: %w", err)
	}
	resp, err := m.secretsClient.Get(ctx, &secretsv1.GetRequest{Ref: ref})
	if err != nil {
		return "", fmt.Errorf("reading the API key: %w", err)
	}
	return resp.GetValue(), nil
}

// Provision creates (or finds again, EnsureVM is idempotent) powerdns's
// dedicated VM.
func (m *powerdnsModule) Provision(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
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
	state["domain"] = domain(req)
	s, err := sdk.NewState(state)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

// connTarget holds the connection details of a target VM, independently of the
// protobuf type of the function that consumes them (ansiblev1.Target and
// osbasev1.Target carry the same fields but are distinct types).
type connTarget struct {
	Host           string
	Port           int32
	User           string
	PrivateKey     string
	CertificatePem string
}

func (m *powerdnsModule) targetFromState(ctx context.Context, req *modulev1.StepRequest, pair sshKeyPair) (connTarget, error) {
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
	return connTarget{
		Host:       fmt.Sprint(state["vm_ip"]),
		Port:       int32(port),
		User:       sshUser,
		PrivateKey: pair.PrivateKeyOpenSSH,
	}, nil
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

// Configure installs PowerDNS (authoritative + recursor), points the VM at the
// active NTP and resolver (time.ntp/v1, dns.resolver/v1 — both already real at
// this point of milestone M6, unlike chrony, which had no provider yet), then
// wires the HTTP API client.
func (m *powerdnsModule) Configure(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
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

	apiKey, err := m.apiKeySecret(ctx, name)
	if err != nil {
		return nil, err
	}

	vars, err := sdk.NewState(map[string]any{
		"api_key":            apiKey,
		"domain":             dom,
		"upstream_resolvers": []any{"9.9.9.9", "1.1.1.1"},
	})
	if err != nil {
		return nil, err
	}
	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target:       target.ansible(),
		PlaybookYaml: installPowerDNSPlaybook,
		Vars:         vars,
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetOk() {
		return nil, fmt.Errorf("Configure(powerdns) failed:\n%s", resp.GetOutput())
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

	m.zoneServer.mu.Lock()
	m.zoneServer.client = newPDNSClient(fmt.Sprintf("http://%s:%d", target.Host, pdnsAPIPort), apiKey)
	m.zoneServer.vmIP = target.Host
	m.zoneServer.domain = dom
	m.zoneServer.mu.Unlock()

	s, err := sdk.NewState(modState)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

// Handover reads dns.zone/v1@seed (coredns) back and recreates each record
// through its own dns.zone/v1 implementation, then compares
// (docs/07-mvp-modules.md: "reads the zone back through
// dns.zone@seed.ListRecords, recreates everything, compares").
func (m *powerdnsModule) Handover(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	dom := domain(req)

	seedRecords, err := m.dnsZoneSeedClient.ListRecords(ctx, &dnszonev1.Zone{Zone: dom})
	if err != nil {
		return nil, fmt.Errorf("reading dns.zone/v1@seed: %w", err)
	}

	for _, r := range seedRecords.GetRecords() {
		if _, err := m.zoneServer.UpsertRecord(ctx, r); err != nil {
			return nil, fmt.Errorf("recreating %s %s: %w", r.GetType(), r.GetName(), err)
		}
	}

	ownRecords, err := m.zoneServer.ListRecords(ctx, &dnszonev1.Zone{Zone: dom})
	if err != nil {
		return nil, err
	}
	if !recordsEqual(seedRecords.GetRecords(), ownRecords.GetRecords()) {
		return nil, fmt.Errorf("handover: the recreated records (%d) do not match the seed's (%d)",
			len(ownRecords.GetRecords()), len(seedRecords.GetRecords()))
	}

	state := sdk.StateMap(req.GetState())
	state["handed_over"] = true
	s, err := sdk.NewState(state)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func recordKey(r *dnszonev1.Record) string {
	values := append([]string(nil), r.GetValues()...)
	sortStrings(values)
	return fmt.Sprintf("%s|%s|%s|%s|%d", r.GetZone(), r.GetName(), r.GetType(), strings.Join(values, ","), r.GetTtl())
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func recordsEqual(a, b []*dnszonev1.Record) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, r := range a {
		set[recordKey(r)] = true
	}
	for _, r := range b {
		if !set[recordKey(r)] {
			return false
		}
	}
	return true
}

// Verify proves, from a disposable third-party VM, a real forward and reverse
// resolution, plus an external name through the recursor
// (docs/07-mvp-modules.md, docs/03-module-contract.md rule 2).
func (m *powerdnsModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	dom := domain(req)

	if _, err := m.zoneServer.UpsertRecord(ctx, &dnszonev1.Record{
		Zone: dom, Name: verifyProbeName, Type: "A", Values: []string{verifyProbeIP}, Ttl: 300,
	}); err != nil {
		return nil, fmt.Errorf("Verify(powerdns): forward probe: %w", err)
	}
	defer func() {
		_, _ = m.zoneServer.DeleteRecord(context.Background(), &dnszonev1.RecordKey{Zone: dom, Name: verifyProbeName, Type: "A"})
	}()

	if _, err := m.zoneServer.UpsertRecord(ctx, &dnszonev1.Record{
		Zone: verifyReverseZ, Name: verifyReverseIP1, Type: "PTR", Values: []string{fqdn(verifyProbeName + "." + dom)}, Ttl: 300,
	}); err != nil {
		return nil, fmt.Errorf("Verify(powerdns): reverse probe: %w", err)
	}
	defer func() {
		_, _ = m.zoneServer.DeleteRecord(context.Background(), &dnszonev1.RecordKey{Zone: verifyReverseZ, Name: verifyReverseIP1, Type: "PTR"})
	}()

	verifierName := name + verifierSuffix
	pair, err := m.sshKeyPair(ctx, verifierName)
	if err != nil {
		return nil, err
	}
	verifierVM, err := m.vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name: verifierName, Env: verifierName, SshPublicKey: pair.PublicKeyAuthorized, User: sshUser,
	})
	if err != nil {
		return nil, fmt.Errorf("EnsureVM(%q) (verifier): %w", verifierName, err)
	}
	defer func() {
		_, _ = m.vmClient.DeleteVM(context.Background(), &computevmv1.DeleteVMRequest{Name: verifierName})
	}()

	serverIP := fmt.Sprint(sdk.StateMap(req.GetState())["vm_ip"])
	target := &ansiblev1.Target{
		Host: verifierVM.GetIp(), Port: verifierVM.GetSshPort(), User: sshUser, SshPrivateKey: pair.PrivateKeyOpenSSH,
	}
	vars, err := sdk.NewState(map[string]any{
		"dns_server":    serverIP,
		"forward_name":  verifyProbeName + "." + dom,
		"reverse_ip":    verifyProbeIP,
		"external_name": "example.com",
	})
	if err != nil {
		return nil, err
	}

	resp, err := m.ansibleClient.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target:       target,
		PlaybookYaml: checkResolutionPlaybook,
		Vars:         vars,
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetOk() {
		return nil, fmt.Errorf("Verify(powerdns) failed:\n%s", resp.GetOutput())
	}

	forward, reverse, external, err := parseResolutionOutput(resp.GetOutput())
	if err != nil {
		return nil, fmt.Errorf("Verify(powerdns): %w\noutput:\n%s", err, resp.GetOutput())
	}
	if !strings.Contains(forward, verifyProbeIP) {
		return nil, fmt.Errorf("Verify(powerdns): forward resolution = %q, expected to contain %q", forward, verifyProbeIP)
	}
	if !strings.Contains(reverse, verifyProbeName) {
		return nil, fmt.Errorf("Verify(powerdns): reverse resolution = %q, expected to contain %q", reverse, verifyProbeName)
	}
	if strings.TrimSpace(external) == "" {
		return nil, fmt.Errorf("Verify(powerdns): empty resolution of an external name (recursor not working?)")
	}

	state := sdk.StateMap(req.GetState())
	state["verified"] = true
	s, err := sdk.NewState(state)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

var (
	forwardRe  = regexp.MustCompile(`"forward":\s*"([^"]*)"`)
	reverseRe  = regexp.MustCompile(`"reverse":\s*"([^"]*)"`)
	externalRe = regexp.MustCompile(`"external":\s*"([^"]*)"`)
)

func parseResolutionOutput(output string) (forward, reverse, external string, err error) {
	f := forwardRe.FindStringSubmatch(output)
	r := reverseRe.FindStringSubmatch(output)
	e := externalRe.FindStringSubmatch(output)
	if f == nil || r == nil || e == nil {
		return "", "", "", fmt.Errorf("forward/reverse/external results not found in the output of check_resolution.yml")
	}
	return f[1], r[1], e[1], nil
}

func (m *powerdnsModule) Destroy(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	if _, err := m.vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: name}); err != nil {
		return nil, fmt.Errorf("DeleteVM(%q): %w", name, err)
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

// powerdnsZoneServer implements dns.zone/v1 by driving the PowerDNS REST API
// directly (api.go) — dns.resolver/v1 (resolverAdapter, below) wraps it to
// read the same access point (the same pattern as modules/coredns).
type powerdnsZoneServer struct {
	dnszonev1.UnimplementedDnsZoneServer

	mu     sync.Mutex
	client *pdnsClient
	vmIP   string
	domain string
}

func (s *powerdnsZoneServer) ready() (*pdnsClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil, fmt.Errorf("dns.zone/v1: powerdns not configured yet (Configure has not succeeded yet)")
	}
	return s.client, nil
}

func recordName(zone, name string) string {
	if name == "" || name == "@" {
		return fqdn(zone)
	}
	return fqdn(name + "." + zone)
}

func (s *powerdnsZoneServer) UpsertRecord(ctx context.Context, req *dnszonev1.Record) (*dnszonev1.Empty, error) {
	client, err := s.ready()
	if err != nil {
		return nil, err
	}
	if err := client.ensureZone(ctx, req.GetZone()); err != nil {
		return nil, err
	}
	ttl := req.GetTtl()
	if ttl == 0 {
		ttl = 300
	}
	records := make([]pdnsRecordContent, 0, len(req.GetValues()))
	for _, v := range req.GetValues() {
		records = append(records, pdnsRecordContent{Content: v})
	}
	rrset := pdnsRRset{
		Name: recordName(req.GetZone(), req.GetName()), Type: req.GetType(), TTL: ttl,
		ChangeType: "REPLACE", Records: records,
	}
	if err := client.patchRRset(ctx, req.GetZone(), rrset); err != nil {
		return nil, err
	}
	return &dnszonev1.Empty{}, nil
}

func (s *powerdnsZoneServer) DeleteRecord(ctx context.Context, req *dnszonev1.RecordKey) (*dnszonev1.Empty, error) {
	client, err := s.ready()
	if err != nil {
		return nil, err
	}
	rrset := pdnsRRset{
		Name: recordName(req.GetZone(), req.GetName()), Type: req.GetType(), ChangeType: "DELETE",
	}
	if err := client.patchRRset(ctx, req.GetZone(), rrset); err != nil {
		return nil, err
	}
	return &dnszonev1.Empty{}, nil
}

func (s *powerdnsZoneServer) ListRecords(ctx context.Context, req *dnszonev1.Zone) (*dnszonev1.Records, error) {
	client, err := s.ready()
	if err != nil {
		return nil, err
	}
	rrsets, err := client.listRRsets(ctx, req.GetZone())
	if err != nil {
		return nil, err
	}
	apex := fqdn(req.GetZone())
	var out []*dnszonev1.Record
	for _, rr := range rrsets {
		if rr.Name == apex && (rr.Type == "SOA" || rr.Type == "NS") {
			continue // created automatically by PowerDNS with the zone, not user records.
		}
		name := strings.TrimSuffix(rr.Name, ".")
		name = strings.TrimSuffix(name, "."+req.GetZone())
		values := make([]string, 0, len(rr.Records))
		for _, r := range rr.Records {
			values = append(values, r.Content)
		}
		out = append(out, &dnszonev1.Record{Zone: req.GetZone(), Name: name, Type: rr.Type, Values: values, Ttl: rr.TTL})
	}
	return &dnszonev1.Records{Records: out}, nil
}

func (s *powerdnsZoneServer) Endpoint(context.Context, *dnszonev1.Empty) (*dnszonev1.EndpointInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &dnszonev1.EndpointInfo{Address: s.vmIP, Port: 53}, nil
}

// resolverAdapter implements dns.resolver/v1 by reading the same access point
// as dns.zone/v1 — the recursor runs on the same VM, port 53 (the same pattern
// as modules/coredns).
type resolverAdapter struct {
	dnsresolverv1.UnimplementedDnsResolverServer
	*powerdnsZoneServer
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
	module := &powerdnsModule{manifest: mf.ToProto()}
	zoneServer := &powerdnsZoneServer{}
	module.zoneServer = zoneServer

	sdk.Serve(module,
		sdk.FunctionProvider{
			Name:     "dns.zone/v1",
			Register: func(s *grpc.Server) { dnszonev1.RegisterDnsZoneServer(s, zoneServer) },
		},
		sdk.FunctionProvider{
			Name: "dns.resolver/v1",
			Register: func(s *grpc.Server) {
				dnsresolverv1.RegisterDnsResolverServer(s, resolverAdapter{powerdnsZoneServer: zoneServer})
			},
		},
	)
}
