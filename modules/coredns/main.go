// SPDX-License-Identifier: Apache-2.0

// coredns fournit dns.zone/v1 et dns.resolver/v1 en phase graine
// (docs/07-modules-mvp.md), en conteneur sur la graine (core.container/v1).
// La zone est régénérée (fichier BIND réécrit, conteneur redémarré) à
// chaque UpsertRecord/DeleteRecord.
//
// Portée assumée pour ce jalon : les enregistrements vivent en mémoire dans
// le process du module, pas dans l'état du cœur (docs/03-contrat-module.md
// §4 règle 6 vise avant tout la persistance inter-redémarrage pour l'état
// de cycle de vie ; UpsertRecord/DeleteRecord sont des appels de fonction,
// sans StepRequest.state à travers lequel passer). Un kill+relance du cœur
// perdrait donc la zone accumulée — noté en dette, docs/PROGRESS.md.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	containerv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/container/v1"
	dnsresolverv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/resolver/v1"
	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

const corednsImage = "coredns/coredns:latest"

type coreDNSModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest

	mu              sync.Mutex
	broker          *sdk.BrokerClient
	brokerToken     string
	containerClient containerv1.ContainerClient
	zoneServer      *dnsZoneServer
}

func (m *coreDNSModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *coreDNSModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *coreDNSModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

// Check : conforme une fois amorcé (seeded) ou déjà retiré par une
// passation (retired) — coredns ne fournit rien en phase cible, son
// itération de plan (internal/engine) s'arrête donc à seed_ready
// (docs/03-contrat-module.md §5) ; le jeton de session est capturé pour
// que les gestionnaires de fonction (UpsertRecord...) puissent joindre
// core.container/v1.
func (m *coreDNSModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	m.brokerToken = req.GetBrokerToken()
	flags := sdk.StateMap(req.GetState())
	if boolFlag(flags, "seeded") || boolFlag(flags, "retired") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_CONFORME}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_A_FAIRE}, nil
}

func (m *coreDNSModule) SeedUp(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return m.setFlag(req, "seeded")
}

// Verify : rien de spécifique au produit à vérifier avant qu'une zone
// existe (UpsertRecord n'a encore jamais été appelé à ce stade du cycle de
// vie) — la résolution DNS réelle est prouvée du point de vue consommateur
// dans internal/modulehost/coredns_test.go, une fois des enregistrements
// présents.
func (m *coreDNSModule) Verify(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return m.setFlag(req, "verified")
}

// SeedDown arrête réellement le conteneur CoreDNS — déclenché par la
// passation d'un module cible (ex. powerdns), pas par sa propre itération
// de plan (docs/08-jalons.md, J6 : "après passation, arrêt de coredns sans
// impact").
func (m *coreDNSModule) SeedDown(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.zoneServer.stopContainer(ctx); err != nil {
		return nil, err
	}
	return m.setFlag(req, "retired")
}

// Destroy : même nettoyage que SeedDown, pour un retrait de la graine hors
// passation (ex. `genesis destroy` sans module cible installé).
func (m *coreDNSModule) Destroy(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.zoneServer.stopContainer(ctx); err != nil {
		return nil, err
	}
	s, err := sdk.NewState(map[string]any{})
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func (m *coreDNSModule) setFlag(req *modulev1.StepRequest, flag string) (*modulev1.StepResult, error) {
	flags := sdk.StateMap(req.GetState())
	flags[flag] = true
	s, err := sdk.NewState(flags)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func boolFlag(flags map[string]any, key string) bool {
	v, _ := flags[key].(bool)
	return v
}

// containers dial la session de broker au plus une fois (Dial ne réussit
// qu'une fois par jeton) et met le client en cache — même précaution que
// modules/base-os et modules/fake-compute.
func (m *coreDNSModule) containers() (containerv1.ContainerClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.containerClient != nil {
		return m.containerClient, nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return nil, fmt.Errorf("coredns : aucune session de broker (Check n'a pas encore été appelé)")
	}
	conn, err := m.broker.Dial(m.brokerToken)
	if err != nil {
		return nil, fmt.Errorf("connexion à core.container/v1 : %w", err)
	}
	m.containerClient = containerv1.NewContainerClient(conn)
	return m.containerClient, nil
}

// dnsZoneServer implémente dns.zone/v1. dns.resolver/v1 (resolverAdapter,
// plus bas) l'enveloppe pour lire le même point d'accès (docs/07 : coredns
// fait office des deux fonctions à la fois).
type dnsZoneServer struct {
	dnszonev1.UnimplementedDnsZoneServer
	module *coreDNSModule

	mu          sync.Mutex
	records     map[string]*dnszonev1.Record // clé : zone|name|type
	containerID string
	ip          string
}

func recordKey(zone, name, typ string) string {
	return zone + "|" + name + "|" + typ
}

func (s *dnsZoneServer) UpsertRecord(ctx context.Context, req *dnszonev1.Record) (*dnszonev1.Empty, error) {
	s.mu.Lock()
	if s.records == nil {
		s.records = map[string]*dnszonev1.Record{}
	}
	s.records[recordKey(req.GetZone(), req.GetName(), req.GetType())] = req
	s.mu.Unlock()

	if err := s.reload(ctx); err != nil {
		return nil, err
	}
	return &dnszonev1.Empty{}, nil
}

func (s *dnsZoneServer) DeleteRecord(ctx context.Context, req *dnszonev1.RecordKey) (*dnszonev1.Empty, error) {
	s.mu.Lock()
	delete(s.records, recordKey(req.GetZone(), req.GetName(), req.GetType()))
	s.mu.Unlock()

	if err := s.reload(ctx); err != nil {
		return nil, err
	}
	return &dnszonev1.Empty{}, nil
}

func (s *dnsZoneServer) ListRecords(_ context.Context, req *dnszonev1.Zone) (*dnszonev1.Records, error) {
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

func (s *dnsZoneServer) Endpoint(context.Context, *dnszonev1.Empty) (*dnszonev1.EndpointInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &dnszonev1.EndpointInfo{Address: s.ip, Port: 53}, nil
}

// stopContainer arrête réellement le conteneur CoreDNS s'il tourne —
// idempotent (aucun conteneur démarré : no-op, ex. passation avant tout
// UpsertRecord). Appelé par SeedDown et Destroy.
func (s *dnsZoneServer) stopContainer(ctx context.Context) error {
	s.mu.Lock()
	id := s.containerID
	s.mu.Unlock()
	if id == "" {
		return nil
	}
	containers, err := s.module.containers()
	if err != nil {
		return err
	}
	if _, err := containers.Stop(ctx, &containerv1.StopRequest{ContainerId: id}); err != nil {
		return fmt.Errorf("arrêt du conteneur CoreDNS : %w", err)
	}
	s.mu.Lock()
	s.containerID = ""
	s.ip = ""
	s.mu.Unlock()
	return nil
}

// reload régénère le Corefile et les fichiers de zone, puis redémarre le
// conteneur CoreDNS (docs/07-modules-mvp.md : "Zone régénérée à chaque
// UpsertRecord").
func (s *dnsZoneServer) reload(ctx context.Context) error {
	s.mu.Lock()
	zones := map[string][]*dnszonev1.Record{}
	for _, r := range s.records {
		zones[r.GetZone()] = append(zones[r.GetZone()], r)
	}
	oldContainerID := s.containerID
	s.mu.Unlock()

	dir, err := os.MkdirTemp("", "genesis-coredns-*")
	if err != nil {
		return fmt.Errorf("préparation du répertoire de zone : %w", err)
	}
	// Chmod : le conteneur coredns tourne sous son propre UID interne
	// (même précaution que core.ansible/v1, docs/PROGRESS.md J5).
	if err := os.Chmod(dir, 0o755); err != nil {
		return fmt.Errorf("permissions du répertoire de zone : %w", err)
	}

	zoneNames := make([]string, 0, len(zones))
	for zone := range zones {
		zoneNames = append(zoneNames, zone)
	}
	sort.Strings(zoneNames)

	var corefile strings.Builder
	for _, zone := range zoneNames {
		fmt.Fprintf(&corefile, "%s:53 {\n    file /zones/db.%s\n    log\n}\n", zone, zone)
		content := zoneFileContent(zone, zones[zone])
		if err := os.WriteFile(filepath.Join(dir, "db."+zone), []byte(content), 0o644); err != nil {
			return fmt.Errorf("écriture de la zone %q : %w", zone, err)
		}
	}
	if len(zoneNames) == 0 {
		// Corefile minimal valide tant qu'aucune zone n'a encore de
		// enregistrement (premier appel possible avant tout UpsertRecord).
		corefile.WriteString(".:53 {\n    health\n}\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "Corefile"), []byte(corefile.String()), 0o644); err != nil {
		return fmt.Errorf("écriture du Corefile : %w", err)
	}

	containers, err := s.module.containers()
	if err != nil {
		return err
	}

	if oldContainerID != "" {
		if _, err := containers.Stop(ctx, &containerv1.StopRequest{ContainerId: oldContainerID}); err != nil {
			return fmt.Errorf("arrêt de l'ancien conteneur CoreDNS : %w", err)
		}
	}

	resp, err := containers.Run(ctx, &containerv1.RunRequest{
		Name:    "genesis-coredns",
		Image:   corednsImage,
		Command: []string{"-conf", "/zones/Corefile"},
		Mounts:  []*containerv1.Mount{{HostPath: dir, ContainerPath: "/zones"}},
		Detach:  true,
	})
	if err != nil {
		return fmt.Errorf("démarrage de CoreDNS : %w", err)
	}

	s.mu.Lock()
	s.containerID = resp.GetContainerId()
	s.ip = resp.GetIp()
	s.mu.Unlock()
	return nil
}

// zoneFileContent génère un fichier de zone BIND minimal valide pour le
// plugin "file" de CoreDNS.
func zoneFileContent(zone string, records []*dnszonev1.Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "$ORIGIN %s.\n$TTL 300\n", zone)
	fmt.Fprintf(&b, "@ IN SOA ns.%s. admin.%s. (%d 3600 900 604800 300)\n", zone, zone, time.Now().Unix())
	fmt.Fprintf(&b, "@ IN NS ns.%s.\n", zone)
	for _, r := range records {
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

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}

	module := &coreDNSModule{manifest: mf.ToProto()}
	zoneServer := &dnsZoneServer{module: module}
	module.zoneServer = zoneServer

	sdk.Serve(module,
		sdk.FunctionProvider{
			Name:     "dns.zone/v1",
			Register: func(s *grpc.Server) { dnszonev1.RegisterDnsZoneServer(s, zoneServer) },
		},
		sdk.FunctionProvider{
			Name: "dns.resolver/v1",
			Register: func(s *grpc.Server) {
				dnsresolverv1.RegisterDnsResolverServer(s, resolverAdapter{dnsZoneServer: zoneServer})
			},
		},
	)
}

// resolverAdapter implémente dns.resolver/v1 en lisant le même point
// d'accès que dns.zone/v1 (champs de dnsZoneServer promus par l'embedding).
type resolverAdapter struct {
	dnsresolverv1.UnimplementedDnsResolverServer
	*dnsZoneServer
}

func (r resolverAdapter) Endpoint(context.Context, *dnsresolverv1.Empty) (*dnsresolverv1.EndpointInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &dnsresolverv1.EndpointInfo{Address: r.ip, Port: 53}, nil
}
