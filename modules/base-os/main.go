// SPDX-License-Identifier: Apache-2.0

// base-os provides os.base/v1 (docs/07-mvp-modules.md): the functional
// configuration a target VM needs to work (CA, resolver, NTP). Harden (SSH
// hardening, updates, nftables) is a pure security concern deferred to a
// future iteration — see the scope discussion of milestone M5,
// docs/PROGRESS.md.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	osbasev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

//go:embed playbooks/trust_ca.yml
var trustCAPlaybook []byte

//go:embed playbooks/set_resolver.yml
var setResolverPlaybook []byte

//go:embed playbooks/set_ntp.yml
var setNTPPlaybook []byte

// baseOSModule carries the module's lifecycle. It has nothing to provision for
// itself: Check is always compliant, and all the work happens in osBaseServer
// in response to calls to the os.base/v1 function.
type baseOSModule struct {
	modulev1.UnimplementedModuleServer
	manifest    *modulev1.Manifest
	broker      *sdk.BrokerClient
	brokerToken string

	mu            sync.Mutex
	ansibleClient ansiblev1.AnsibleClient // cached: Dial only succeeds once per session
}

func (m *baseOSModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *baseOSModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *baseOSModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

// Check captures the broker session token of the current step: it is base-os's
// only opportunity to get access (scoped to its own requires) to
// core.ansible/v1, which its function handlers then reuse
// (docs/02-architecture.md: the broker routes per caller, not per function).
func (m *baseOSModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	m.brokerToken = req.GetBrokerToken()
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
}

// dialAnsible dials the broker session at most once: Dial can only succeed
// once per token (the connection info is sent only once on the core side), but
// the resulting gRPC connection supports many calls — it is cached and reused
// by TrustCA/SetResolver/SetNTP.
func (m *baseOSModule) dialAnsible() (ansiblev1.AnsibleClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ansibleClient != nil {
		return m.ansibleClient, nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return nil, fmt.Errorf("base-os: no broker session (Check has not been called on this module yet)")
	}
	conn, err := m.broker.Dial(m.brokerToken)
	if err != nil {
		return nil, fmt.Errorf("connecting to core.ansible/v1: %w", err)
	}
	m.ansibleClient = ansiblev1.NewAnsibleClient(conn)
	return m.ansibleClient, nil
}

// osBaseServer implements the os.base/v1 function.
type osBaseServer struct {
	osbasev1.UnimplementedBaseServer
	module *baseOSModule
}

func toAnsibleTarget(t *osbasev1.Target) *ansiblev1.Target {
	return &ansiblev1.Target{
		Host:          t.GetHost(),
		Port:          t.GetPort(),
		User:          t.GetUser(),
		SshPrivateKey: t.GetSshPrivateKey(),
	}
}

func (s *osBaseServer) Harden(context.Context, *osbasev1.HardenRequest) (*osbasev1.HardenResponse, error) {
	return nil, status.Error(codes.Unimplemented, "Harden: pure hardening deferred to a future iteration (docs/PROGRESS.md)")
}

func (s *osBaseServer) TrustCA(ctx context.Context, req *osbasev1.TrustCARequest) (*osbasev1.TrustCAResponse, error) {
	client, err := s.module.dialAnsible()
	if err != nil {
		return nil, err
	}
	vars, err := sdk.NewState(map[string]any{"ca_cert_pem": req.GetCaCertPem()})
	if err != nil {
		return nil, err
	}
	resp, err := client.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target:       toAnsibleTarget(req.GetTarget()),
		PlaybookYaml: trustCAPlaybook,
		Vars:         vars,
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetOk() {
		return nil, fmt.Errorf("TrustCA failed:\n%s", resp.GetOutput())
	}
	return &osbasev1.TrustCAResponse{}, nil
}

func (s *osBaseServer) SetResolver(ctx context.Context, req *osbasev1.SetResolverRequest) (*osbasev1.SetResolverResponse, error) {
	client, err := s.module.dialAnsible()
	if err != nil {
		return nil, err
	}
	nameservers := make([]any, len(req.GetNameservers()))
	for i, ns := range req.GetNameservers() {
		nameservers[i] = ns
	}
	vars, err := sdk.NewState(map[string]any{"nameservers": nameservers, "domain": req.GetDomain()})
	if err != nil {
		return nil, err
	}
	resp, err := client.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target:       toAnsibleTarget(req.GetTarget()),
		PlaybookYaml: setResolverPlaybook,
		Vars:         vars,
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetOk() {
		return nil, fmt.Errorf("SetResolver failed:\n%s", resp.GetOutput())
	}
	return &osbasev1.SetResolverResponse{}, nil
}

func (s *osBaseServer) SetNTP(ctx context.Context, req *osbasev1.SetNTPRequest) (*osbasev1.SetNTPResponse, error) {
	client, err := s.module.dialAnsible()
	if err != nil {
		return nil, err
	}
	servers := make([]any, len(req.GetServers()))
	for i, srv := range req.GetServers() {
		servers[i] = srv
	}
	vars, err := sdk.NewState(map[string]any{"ntp_servers": servers})
	if err != nil {
		return nil, err
	}
	resp, err := client.RunPlaybook(ctx, &ansiblev1.RunPlaybookRequest{
		Target:       toAnsibleTarget(req.GetTarget()),
		PlaybookYaml: setNTPPlaybook,
		Vars:         vars,
	})
	if err != nil {
		return nil, err
	}
	if !resp.GetOk() {
		return nil, fmt.Errorf("SetNTP failed:\n%s", resp.GetOutput())
	}
	return &osbasev1.SetNTPResponse{}, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	module := &baseOSModule{manifest: mf.ToProto()}
	osBase := &osBaseServer{module: module}
	sdk.Serve(module, sdk.FunctionProvider{
		Name:     "os.base/v1",
		Register: func(s *grpc.Server) { osbasev1.RegisterBaseServer(s, osBase) },
	})
}
