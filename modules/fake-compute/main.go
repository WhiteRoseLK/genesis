// SPDX-License-Identifier: Apache-2.0

// fake-compute fournit compute.vm/v1 via de vrais conteneurs SSH-joignables
// sur la graine (docs/07-mvp-modules.md), pour tester le cœur et les
// modules de service sans hyperviseur. Chaque « VM » est un conteneur
// lscr.io/linuxserver/openssh-server réel, démarré via core.container/v1 —
// pas de systemd à l'intérieur (contrairement à la lettre du doc 07) : le
// point du module est d'être une cible SSH réelle pour les autres modules,
// pas de reproduire fidèlement un système d'exploitation complet.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	containerv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/container/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

// sshTargetImage et le port interne sont fixés : le point de fake-compute
// est d'être une cible SSH prévisible, pas configurable (docs/08-milestones.md, J4/J6).
const (
	sshTargetImage = "lscr.io/linuxserver/openssh-server:10.3_p1-r1-ls237@sha256:946fa26105e0ec212fdf821b9ddc59aab65f2c2d07c02b25ff0f5001fc332ff0"
	sshTargetPort  = 22
)

type fakeComputeModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest

	mu              sync.Mutex
	broker          *sdk.BrokerClient
	brokerToken     string
	containerClient containerv1.ContainerClient
}

func (m *fakeComputeModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *fakeComputeModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *fakeComputeModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *fakeComputeModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	// Rien à provisionner pour le module lui-même : les VM factices sont
	// créées à la demande via la fonction compute.vm/v1. Capture le jeton
	// de session pour joindre core.container/v1 (même mécanisme que
	// modules/base-os avec core.ansible/v1).
	m.brokerToken = req.GetBrokerToken()
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
}

func (m *fakeComputeModule) Provision(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return ok(req)
}

func (m *fakeComputeModule) Configure(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return ok(req)
}

func (m *fakeComputeModule) Verify(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return ok(req)
}

func (m *fakeComputeModule) Destroy(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return ok(req)
}

func ok(req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	s, err := sdk.NewState(sdk.StateMap(req.GetState()))
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

// containers dial la session de broker au plus une fois (Dial ne réussit
// qu'une fois par jeton) et met le client en cache pour tous les appels
// suivants — même précaution que modules/base-os.
func (m *fakeComputeModule) containers() (containerv1.ContainerClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.containerClient != nil {
		return m.containerClient, nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return nil, fmt.Errorf("fake-compute : aucune session de broker (Check n'a pas encore été appelé)")
	}
	conn, err := m.broker.Dial(m.brokerToken)
	if err != nil {
		return nil, fmt.Errorf("connexion à core.container/v1 : %w", err)
	}
	m.containerClient = containerv1.NewContainerClient(conn)
	return m.containerClient, nil
}

// computeVMServer implémente functions/compute/vm/v1.
type computeVMServer struct {
	computevmv1.UnimplementedComputeVMServer
	module *fakeComputeModule

	mu  sync.Mutex
	vms map[string]*computevmv1.VM
}

func newComputeVMServer(module *fakeComputeModule) *computeVMServer {
	return &computeVMServer{module: module, vms: map[string]*computevmv1.VM{}}
}

func (s *computeVMServer) EnsureImage(context.Context, *computevmv1.EnsureImageRequest) (*computevmv1.EnsureImageResponse, error) {
	return &computevmv1.EnsureImageResponse{}, nil
}

// EnsureVM est idempotent par nom (clé d'idempotence, docs/07-mvp-modules.md).
func (s *computeVMServer) EnsureVM(ctx context.Context, req *computevmv1.EnsureVMRequest) (*computevmv1.VM, error) {
	s.mu.Lock()
	if vm, ok := s.vms[req.GetName()]; ok {
		s.mu.Unlock()
		return vm, nil
	}
	s.mu.Unlock()

	containers, err := s.module.containers()
	if err != nil {
		return nil, err
	}

	user := req.GetUser()
	if user == "" {
		user = "genesis"
	}

	resp, err := containers.Run(ctx, &containerv1.RunRequest{
		Name:  "genesis-vm-" + req.GetName(),
		Image: sshTargetImage,
		Env: map[string]string{
			"PUBLIC_KEY":      req.GetSshPublicKey(),
			"USER_NAME":       user,
			"PASSWORD_ACCESS": "false",
			"SUDO_ACCESS":     "true",
			"LISTEN_PORT":     fmt.Sprintf("%d", sshTargetPort),
		},
		Detach: true,
	})
	if err != nil {
		return nil, fmt.Errorf("démarrage de la VM %q : %w", req.GetName(), err)
	}

	vm := &computevmv1.VM{
		Id:      resp.GetContainerId(),
		Name:    req.GetName(),
		Ip:      resp.GetIp(),
		Status:  "running",
		SshPort: sshTargetPort,
	}

	s.mu.Lock()
	s.vms[req.GetName()] = vm
	s.mu.Unlock()

	return vm, nil
}

func (s *computeVMServer) GetVM(_ context.Context, req *computevmv1.GetVMRequest) (*computevmv1.VM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	vm, ok := s.vms[req.GetName()]
	if !ok {
		return nil, fmt.Errorf("VM %q introuvable", req.GetName())
	}
	return vm, nil
}

func (s *computeVMServer) DeleteVM(ctx context.Context, req *computevmv1.DeleteVMRequest) (*computevmv1.DeleteVMResponse, error) {
	s.mu.Lock()
	vm, ok := s.vms[req.GetName()]
	if ok {
		delete(s.vms, req.GetName())
	}
	s.mu.Unlock()

	if !ok {
		return &computevmv1.DeleteVMResponse{}, nil // idempotent : déjà absente
	}

	containers, err := s.module.containers()
	if err != nil {
		return nil, err
	}
	if _, err := containers.Stop(ctx, &containerv1.StopRequest{ContainerId: vm.GetId()}); err != nil {
		return nil, fmt.Errorf("arrêt de la VM %q : %w", req.GetName(), err)
	}
	return &computevmv1.DeleteVMResponse{}, nil
}

func (s *computeVMServer) Now(context.Context, *computevmv1.NowRequest) (*computevmv1.NowResponse, error) {
	return &computevmv1.NowResponse{Time: timestamppb.Now()}, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}

	module := &fakeComputeModule{manifest: mf.ToProto()}
	vmServer := newComputeVMServer(module)
	sdk.Serve(module, sdk.FunctionProvider{
		Name:     "compute.vm/v1",
		Register: func(s *grpc.Server) { computevmv1.RegisterComputeVMServer(s, vmServer) },
	})
}
