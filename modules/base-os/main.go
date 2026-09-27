// SPDX-License-Identifier: Apache-2.0

// base-os fournit os.base/v1 (docs/07-mvp-modules.md) : la configuration
// fonctionnelle nécessaire au bon fonctionnement d'une VM cible (CA,
// résolveur, NTP). Harden (durcissement SSH, mises à jour, nftables) est un
// volet de sécurité pure différé à une itération future — voir la
// discussion de portée du jalon J5, docs/PROGRESS.md.
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

// baseOSModule porte le cycle de vie du module. Il n'a rien à provisionner
// pour lui-même : Check est toujours conforme, tout le travail se fait dans
// osBaseServer en réponse aux appels de la fonction os.base/v1.
type baseOSModule struct {
	modulev1.UnimplementedModuleServer
	manifest    *modulev1.Manifest
	broker      *sdk.BrokerClient
	brokerToken string

	mu            sync.Mutex
	ansibleClient ansiblev1.AnsibleClient // mis en cache : Dial ne réussit qu'une fois par session
}

func (m *baseOSModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *baseOSModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *baseOSModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

// Check capture le jeton de session de broker de l'étape en cours : c'est la
// seule occasion pour base-os d'obtenir un accès (scopé à son propre
// requires) à core.ansible/v1, que ses gestionnaires de fonction
// réutiliseront ensuite (docs/02-architecture.md : le broker route par
// appelant, pas par la fonction elle-même).
func (m *baseOSModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	m.brokerToken = req.GetBrokerToken()
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
}

// dialAnsible dial la session de broker au plus une fois : Dial ne peut
// réussir qu'une seule fois par jeton (les informations de connexion ne
// sont envoyées qu'une fois côté cœur), mais la connexion gRPC obtenue
// supporte de nombreux appels — elle est mise en cache et réutilisée par
// TrustCA/SetResolver/SetNTP.
func (m *baseOSModule) dialAnsible() (ansiblev1.AnsibleClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ansibleClient != nil {
		return m.ansibleClient, nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return nil, fmt.Errorf("base-os : aucune session de broker (Check n'a pas encore été appelé sur ce module)")
	}
	conn, err := m.broker.Dial(m.brokerToken)
	if err != nil {
		return nil, fmt.Errorf("connexion à core.ansible/v1 : %w", err)
	}
	m.ansibleClient = ansiblev1.NewAnsibleClient(conn)
	return m.ansibleClient, nil
}

// osBaseServer implémente la fonction os.base/v1.
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
	return nil, status.Error(codes.Unimplemented, "Harden : durcissement pur différé à une itération future (docs/PROGRESS.md)")
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
		return nil, fmt.Errorf("TrustCA a échoué :\n%s", resp.GetOutput())
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
		return nil, fmt.Errorf("SetResolver a échoué :\n%s", resp.GetOutput())
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
		return nil, fmt.Errorf("SetNTP a échoué :\n%s", resp.GetOutput())
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
