// SPDX-License-Identifier: Apache-2.0

// powerdns fournit dns.zone/v1 et dns.resolver/v1 en phase cible
// (docs/07-modules-mvp.md) : PowerDNS Authoritative (SQLite, API) + Recursor
// sur sa propre VM (compute.vm/v1), reprend la zone de coredns au moment de
// la passation (Handover lit dns.zone/v1@seed, recrée tout, compare).
//
// Portée assumée pour ce jalon : comme modules/coredns, les paramètres de
// connexion à l'API PowerDNS (adresse, clé) vivent en mémoire dans le
// process du module, peuplés par Configure — un redémarrage du cœur en
// cours de cycle de vie les perdrait (même dette que coredns,
// docs/PROGRESS.md).
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
	defaultVMName    = "powerdns01"
	defaultDomain    = "lab.internal"
	sshUser          = "genesis"
	sshKeyRefFmt     = "powerdns/%s/ssh-key"
	apiKeyRefFmt     = "powerdns/%s/api-key"
	verifierSuffix   = "-verify"
	pdnsAPIPort      = 8081
	verifyProbeName  = "verify-probe"
	verifyProbeIP    = "10.255.255.1"
	verifyReverseIP1 = "1"
	verifyReverseZ   = "255.255.10.in-addr.arpa"
)

// sshKeyPair reflète la valeur JSON du générateur GENERATOR_SSH_KEYPAIR
// côté cœur (internal/secrets.SSHKeyPair) — dupliqué ici par son contrat
// JSON, les modules n'important jamais internal/ (même choix que chrony).
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
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_CONFORME}, nil
}

// dial dial la session de broker au plus une fois (Dial ne réussit qu'une
// fois par jeton) et construit tous les clients typés sur la même
// connexion — même précaution que modules/chrony. dns.zone/v1@seed est
// résolu vers le fournisseur graine actif (coredns) via la clé qualifiée du
// registre (internal/engine), dns.resolver/v1 et les autres via leur clé
// active (docs/05-cycle-bootstrap.md).
func (m *powerdnsModule) dial() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vmClient != nil {
		return nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return fmt.Errorf("powerdns : aucune session de broker (Check n'a pas encore été appelé)")
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
	m.dnsZoneSeedClient = dnszonev1.NewDnsZoneClient(conn)
	m.fleetAgentClient = fleetagentv1.NewFleetAgentClient(conn)
	return nil
}

// installFleetAgents appelle fleet.agent/v1.Install(target) (docs/09-decisions.md
// ADR-017) : diffusé vers tout module « de parc » installé (ex. teleport),
// no-op silencieux si aucun n'est présent (fleet.agent/v1 est un requires
// optionnel — Unimplemented est alors la réponse normale du broker, pas
// une erreur), même méthode que modules/chrony.
func (m *powerdnsModule) installFleetAgents(ctx context.Context, target connTarget) error {
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

func (m *powerdnsModule) apiKeySecret(ctx context.Context, name string) (string, error) {
	ref := fmt.Sprintf(apiKeyRefFmt, name)
	if _, err := m.secretsClient.Ensure(ctx, &secretsv1.EnsureRequest{
		Ref:       ref,
		Generator: secretsv1.Generator_GENERATOR_TOKEN,
		Meta:      &secretsv1.Meta{Owner: "powerdns", Consumers: []string{"powerdns"}, Kind: "api-key"},
	}); err != nil {
		return "", fmt.Errorf("génération de la clé API : %w", err)
	}
	resp, err := m.secretsClient.Get(ctx, &secretsv1.GetRequest{Ref: ref})
	if err != nil {
		return "", fmt.Errorf("lecture de la clé API : %w", err)
	}
	return resp.GetValue(), nil
}

// Provision crée (ou retrouve, EnsureVM est idempotent) la VM dédiée de
// powerdns.
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
		return nil, fmt.Errorf("EnsureVM(%q) : %w", name, err)
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

// connTarget porte les coordonnées de connexion à une VM cible, indépendant
// du type protobuf de la fonction qui les consomme (ansiblev1.Target et
// osbasev1.Target portent les mêmes champs mais sont des types distincts).
type connTarget struct {
	Host       string
	Port       int32
	User       string
	PrivateKey string
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
	return connTarget{
		Host:       fmt.Sprint(state["vm_ip"]),
		Port:       int32(port),
		User:       sshUser,
		PrivateKey: pair.PrivateKeyOpenSSH,
	}
}

func (t connTarget) ansible() *ansiblev1.Target {
	return &ansiblev1.Target{Host: t.Host, Port: t.Port, User: t.User, SshPrivateKey: t.PrivateKey}
}

func (t connTarget) osBase() *osbasev1.Target {
	return &osbasev1.Target{Host: t.Host, Port: t.Port, User: t.User, SshPrivateKey: t.PrivateKey}
}

// Configure installe PowerDNS (authoritative + recursor), pointe la VM sur
// le NTP et le résolveur actifs (time.ntp/v1, dns.resolver/v1 — tous deux
// déjà réels à ce stade du jalon J6, contrairement à chrony qui n'avait
// encore aucun fournisseur), puis branche le client HTTP de l'API.
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
	target := targetFromState(req, pair)

	ntpEndpoint, err := m.timeNTPClient.Endpoint(ctx, &timentpv1.Empty{})
	if err != nil {
		return nil, fmt.Errorf("lecture de time.ntp/v1 : %w", err)
	}
	if _, err := m.osBaseClient.SetNTP(ctx, &osbasev1.SetNTPRequest{Target: target.osBase(), Servers: []string{ntpEndpoint.GetAddress()}}); err != nil {
		return nil, fmt.Errorf("SetNTP : %w", err)
	}

	resolverEndpoint, err := m.dnsResolverClient.Endpoint(ctx, &dnsresolverv1.Empty{})
	if err != nil {
		return nil, fmt.Errorf("lecture de dns.resolver/v1 : %w", err)
	}
	if _, err := m.osBaseClient.SetResolver(ctx, &osbasev1.SetResolverRequest{Target: target.osBase(), Nameservers: []string{resolverEndpoint.GetAddress()}, Domain: dom}); err != nil {
		return nil, fmt.Errorf("SetResolver : %w", err)
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
		return nil, fmt.Errorf("Configure(powerdns) a échoué :\n%s", resp.GetOutput())
	}
	if err := m.installFleetAgents(ctx, target); err != nil {
		return nil, err
	}

	m.zoneServer.mu.Lock()
	m.zoneServer.client = newPDNSClient(fmt.Sprintf("http://%s:%d", target.Host, pdnsAPIPort), apiKey)
	m.zoneServer.vmIP = target.Host
	m.zoneServer.domain = dom
	m.zoneServer.mu.Unlock()

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

// Handover relit dns.zone/v1@seed (coredns) et recrée chaque enregistrement
// via sa propre implémentation de dns.zone/v1, puis compare
// (docs/07-modules-mvp.md : "relit la zone via dns.zone@seed.ListRecords,
// recrée tout, compare").
func (m *powerdnsModule) Handover(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	dom := domain(req)

	seedRecords, err := m.dnsZoneSeedClient.ListRecords(ctx, &dnszonev1.Zone{Zone: dom})
	if err != nil {
		return nil, fmt.Errorf("lecture de dns.zone/v1@seed : %w", err)
	}

	for _, r := range seedRecords.GetRecords() {
		if _, err := m.zoneServer.UpsertRecord(ctx, r); err != nil {
			return nil, fmt.Errorf("recréation de %s %s : %w", r.GetType(), r.GetName(), err)
		}
	}

	ownRecords, err := m.zoneServer.ListRecords(ctx, &dnszonev1.Zone{Zone: dom})
	if err != nil {
		return nil, err
	}
	if !recordsEqual(seedRecords.GetRecords(), ownRecords.GetRecords()) {
		return nil, fmt.Errorf("passation : les enregistrements recréés (%d) ne correspondent pas à ceux de la graine (%d)",
			len(ownRecords.GetRecords()), len(seedRecords.GetRecords()))
	}

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
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

// Verify prouve, depuis une VM tierce jetable, une résolution directe et
// inverse réelle, plus un nom externe via le recursor
// (docs/07-modules-mvp.md, docs/03-contrat-module.md règle 2).
func (m *powerdnsModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	dom := domain(req)

	if _, err := m.zoneServer.UpsertRecord(ctx, &dnszonev1.Record{
		Zone: dom, Name: verifyProbeName, Type: "A", Values: []string{verifyProbeIP}, Ttl: 300,
	}); err != nil {
		return nil, fmt.Errorf("Verify(powerdns) : sonde directe : %w", err)
	}
	defer func() {
		_, _ = m.zoneServer.DeleteRecord(context.Background(), &dnszonev1.RecordKey{Zone: dom, Name: verifyProbeName, Type: "A"})
	}()

	if _, err := m.zoneServer.UpsertRecord(ctx, &dnszonev1.Record{
		Zone: verifyReverseZ, Name: verifyReverseIP1, Type: "PTR", Values: []string{fqdn(verifyProbeName + "." + dom)}, Ttl: 300,
	}); err != nil {
		return nil, fmt.Errorf("Verify(powerdns) : sonde inverse : %w", err)
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
		return nil, fmt.Errorf("EnsureVM(%q) (vérificateur) : %w", verifierName, err)
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
		return nil, fmt.Errorf("Verify(powerdns) a échoué :\n%s", resp.GetOutput())
	}

	forward, reverse, external, err := parseResolutionOutput(resp.GetOutput())
	if err != nil {
		return nil, fmt.Errorf("Verify(powerdns) : %w\nsortie :\n%s", err, resp.GetOutput())
	}
	if !strings.Contains(forward, verifyProbeIP) {
		return nil, fmt.Errorf("Verify(powerdns) : résolution directe = %q, attendu de contenir %q", forward, verifyProbeIP)
	}
	if !strings.Contains(reverse, verifyProbeName) {
		return nil, fmt.Errorf("Verify(powerdns) : résolution inverse = %q, attendu de contenir %q", reverse, verifyProbeName)
	}
	if strings.TrimSpace(external) == "" {
		return nil, fmt.Errorf("Verify(powerdns) : résolution d'un nom externe vide (recursor non fonctionnel ?)")
	}

	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
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
		return "", "", "", fmt.Errorf("résultats forward/reverse/external introuvables dans la sortie de check_resolution.yml")
	}
	return f[1], r[1], e[1], nil
}

func (m *powerdnsModule) Destroy(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dial(); err != nil {
		return nil, err
	}
	name := vmName(req)
	if _, err := m.vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: name}); err != nil {
		return nil, fmt.Errorf("DeleteVM(%q) : %w", name, err)
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

// powerdnsZoneServer implémente dns.zone/v1 en pilotant directement l'API
// REST de PowerDNS (api.go) — dns.resolver/v1 (resolverAdapter, plus bas)
// l'enveloppe pour lire le même point d'accès (même schéma que
// modules/coredns).
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
		return nil, fmt.Errorf("dns.zone/v1 : powerdns pas encore configuré (Configure n'a pas encore réussi)")
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
			continue // auto-créés par PowerDNS à la création de la zone, pas des enregistrements de l'utilisateur.
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

// resolverAdapter implémente dns.resolver/v1 en lisant le même point
// d'accès que dns.zone/v1 — le recursor tourne sur la même VM, port 53
// (même schéma que modules/coredns).
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
