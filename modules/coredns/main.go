// SPDX-License-Identifier: Apache-2.0

// coredns provides dns.zone/v1 and dns.resolver/v1 in the seed phase
// (docs/07-mvp-modules.md), as a container on the seed (core.container/v1).
// The zone is regenerated (BIND file rewritten, container restarted) on every
// UpsertRecord/DeleteRecord.
//
// Container connection state (container_id, container_ip) is preserved in
// StepResult.state across lifecycle steps, allowing Check to restore the
// container identity on restart so Endpoint and SeedDown/Destroy target the
// right instance.
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

const corednsImage = "coredns/coredns:1.14.7@sha256:7efd3c635b03efd68c4e8398fc45f0d993d0e9ab016f72c1cefb0fd6d01aa286"

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

// Check: compliant once bootstrapped (seeded) or already retired by a handover
// (retired) — coredns provides nothing in the target phase, so its plan
// iteration (internal/engine) stops at seed_ready (docs/03-module-contract.md
// §5); the session token is captured so that the function handlers
// (UpsertRecord...) can reach core.container/v1.
func (m *coreDNSModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	_ = m.dialWithToken(req.GetBrokerToken())
	flags := sdk.StateMap(req.GetState())
	if cid, ok := flags["container_id"].(string); ok && cid != "" {
		m.zoneServer.mu.Lock()
		if m.zoneServer.containerID == "" {
			m.zoneServer.containerID = cid
			if ip, ok := flags["container_ip"].(string); ok {
				m.zoneServer.ip = ip
			}
		}
		m.zoneServer.mu.Unlock()
	}
	if boolFlag(flags, "seeded") || boolFlag(flags, "retired") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_TODO}, nil
}

func (m *coreDNSModule) SeedUp(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetBrokerToken()); err != nil {
		return nil, err
	}
	m.mu.Lock()
	hasClient := m.containerClient != nil
	m.mu.Unlock()
	if hasClient {
		if err := m.zoneServer.reload(ctx); err != nil {
			return nil, err
		}
	}
	return m.setFlag(req, "seeded")
}

// Verify: nothing product-specific to check before a zone exists (UpsertRecord
// has never been called at this point of the lifecycle) — real DNS resolution
// is proven from a consumer's point of view in
// internal/modulehost/coredns_test.go, once records are present.
func (m *coreDNSModule) Verify(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetBrokerToken()); err != nil {
		return nil, err
	}
	return m.setFlag(req, "verified")
}

// SeedDown really stops the CoreDNS container — triggered by the handover of a
// target module (e.g. powerdns), not by its own plan iteration
// (docs/08-milestones.md, M6: "after the handover, stopping coredns has no
// impact").
func (m *coreDNSModule) SeedDown(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetBrokerToken()); err != nil {
		return nil, err
	}
	flags := sdk.StateMap(req.GetState())
	if cid, ok := flags["container_id"].(string); ok && cid != "" {
		m.zoneServer.mu.Lock()
		if m.zoneServer.containerID == "" {
			m.zoneServer.containerID = cid
		}
		m.zoneServer.mu.Unlock()
	}
	if err := m.zoneServer.stopContainer(ctx); err != nil {
		return nil, err
	}
	delete(flags, "container_id")
	delete(flags, "container_ip")
	flags["retired"] = true
	s, err := sdk.NewState(flags)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

// Destroy: the same cleanup as SeedDown, for a seed removal outside a handover
// (e.g. `genesis destroy` with no target module installed).
func (m *coreDNSModule) Destroy(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetBrokerToken()); err != nil {
		return nil, err
	}
	flags := sdk.StateMap(req.GetState())
	if cid, ok := flags["container_id"].(string); ok && cid != "" {
		m.zoneServer.mu.Lock()
		if m.zoneServer.containerID == "" {
			m.zoneServer.containerID = cid
		}
		m.zoneServer.mu.Unlock()
	}
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
	m.zoneServer.mu.Lock()
	if m.zoneServer.containerID != "" {
		flags["container_id"] = m.zoneServer.containerID
		flags["container_ip"] = m.zoneServer.ip
	}
	m.zoneServer.mu.Unlock()
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

// dialWithToken redials the broker session with the token provided by the
// incoming request.
func (m *coreDNSModule) dialWithToken(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if token == "" {
		if m.containerClient != nil {
			return nil
		}
		return nil
	}
	if m.brokerToken == token && m.containerClient != nil {
		return nil
	}
	if m.broker == nil {
		return nil
	}
	conn, err := m.broker.Dial(token)
	if err != nil {
		return fmt.Errorf("connecting to core.container/v1: %w", err)
	}
	m.brokerToken = token
	m.containerClient = containerv1.NewContainerClient(conn)
	return nil
}

// containers dials the broker session if not already dialed.
func (m *coreDNSModule) containers() (containerv1.ContainerClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.containerClient != nil {
		return m.containerClient, nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return nil, fmt.Errorf("coredns: no broker session (Check has not been called yet)")
	}
	conn, err := m.broker.Dial(m.brokerToken)
	if err != nil {
		return nil, fmt.Errorf("connecting to core.container/v1: %w", err)
	}
	m.containerClient = containerv1.NewContainerClient(conn)
	return m.containerClient, nil
}

// dnsZoneServer implements dns.zone/v1. dns.resolver/v1 (resolverAdapter,
// below) wraps it to read the same access point (docs/07: coredns acts as both
// functions at once).
type dnsZoneServer struct {
	dnszonev1.UnimplementedDnsZoneServer
	module *coreDNSModule

	mu          sync.Mutex
	records     map[string]*dnszonev1.Record // key: zone|name|type
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

// stopContainer really stops the CoreDNS container if it is running —
// idempotent (no container started: no-op, e.g. a handover before any
// UpsertRecord). Called by SeedDown and Destroy.
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
		return fmt.Errorf("stopping the CoreDNS container: %w", err)
	}
	s.mu.Lock()
	s.containerID = ""
	s.ip = ""
	s.mu.Unlock()
	return nil
}

// reload regenerates the Corefile and the zone files, then restarts the
// CoreDNS container (docs/07-mvp-modules.md: "Zone regenerated on every
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
		return fmt.Errorf("preparing the zone directory: %w", err)
	}
	// Chmod: the coredns container runs as its own internal UID (the same
	// precaution as core.ansible/v1, docs/PROGRESS.md M5). Zone data is public
	// by nature (served over DNS), not a secret.
	if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // G302: see above
		return fmt.Errorf("zone directory permissions: %w", err)
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
		if err := os.WriteFile(filepath.Join(dir, "db."+zone), []byte(content), 0o644); err != nil { //nolint:gosec // G306: public DNS zone
			return fmt.Errorf("writing zone %q: %w", zone, err)
		}
	}
	if len(zoneNames) == 0 {
		// Minimal valid Corefile as long as no zone has a record yet (a first
		// call is possible before any UpsertRecord).
		corefile.WriteString(".:53 {\n    health\n}\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "Corefile"), []byte(corefile.String()), 0o644); err != nil { //nolint:gosec // G306: configuration without secrets
		return fmt.Errorf("writing the Corefile: %w", err)
	}

	containers, err := s.module.containers()
	if err != nil {
		return err
	}

	if oldContainerID != "" {
		if _, err := containers.Stop(ctx, &containerv1.StopRequest{ContainerId: oldContainerID}); err != nil {
			return fmt.Errorf("stopping the previous CoreDNS container: %w", err)
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
		return fmt.Errorf("starting CoreDNS: %w", err)
	}

	s.mu.Lock()
	s.containerID = resp.GetContainerId()
	s.ip = resp.GetIp()
	s.mu.Unlock()
	return nil
}

// zoneFileContent generates a minimal valid BIND zone file for CoreDNS's
// "file" plugin.
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

// resolverAdapter implements dns.resolver/v1 by reading the same access point
// as dns.zone/v1 (dnsZoneServer fields promoted through embedding).
type resolverAdapter struct {
	dnsresolverv1.UnimplementedDnsResolverServer
	*dnsZoneServer
}

func (r resolverAdapter) Endpoint(context.Context, *dnsresolverv1.Empty) (*dnsresolverv1.EndpointInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &dnsresolverv1.EndpointInfo{Address: r.ip, Port: 53}, nil
}
