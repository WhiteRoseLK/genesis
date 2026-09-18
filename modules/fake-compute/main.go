// SPDX-License-Identifier: Apache-2.0

// fake-compute fournit compute.vm/v1 en registre mémoire : « VM » factices,
// pour tester le cœur et les modules de service sans hyperviseur
// (docs/07-modules-mvp.md). Pas de conteneurs systemd/SSH réels au jalon J4
// (docs/08-jalons.md) — la fidélité complète au doc 07 arrivera quand un
// module aura réellement besoin de s'y connecter.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	sdk "genesis/sdk/go"
	computevmv1 "genesis/sdk/go/gen/functions/compute/vm/v1"
	modulev1 "genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

type fakeComputeModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest
}

func (m *fakeComputeModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *fakeComputeModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *fakeComputeModule) Check(context.Context, *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	// Rien à provisionner pour le module lui-même : les VM factices sont
	// créées à la demande via la fonction compute.vm/v1, pas par le cycle
	// de vie du module.
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_CONFORME}, nil
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

// computeVMServer implémente functions/compute/vm/v1 en mémoire.
type computeVMServer struct {
	computevmv1.UnimplementedComputeVMServer
	mu  sync.Mutex
	vms map[string]*computevmv1.VM
}

func newComputeVMServer() *computeVMServer {
	return &computeVMServer{vms: map[string]*computevmv1.VM{}}
}

func (s *computeVMServer) EnsureImage(context.Context, *computevmv1.EnsureImageRequest) (*computevmv1.EnsureImageResponse, error) {
	return &computevmv1.EnsureImageResponse{}, nil
}

// EnsureVM est idempotent par nom (clé d'idempotence : nom de VM + tag
// genesis-env, docs/07-modules-mvp.md).
func (s *computeVMServer) EnsureVM(_ context.Context, req *computevmv1.EnsureVMRequest) (*computevmv1.VM, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if vm, ok := s.vms[req.GetName()]; ok {
		return vm, nil
	}
	vm := &computevmv1.VM{
		Id:     "fake-" + req.GetName(),
		Name:   req.GetName(),
		Ip:     fmt.Sprintf("10.0.0.%d", len(s.vms)+2),
		Status: "running",
	}
	s.vms[req.GetName()] = vm
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

func (s *computeVMServer) DeleteVM(_ context.Context, req *computevmv1.DeleteVMRequest) (*computevmv1.DeleteVMResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.vms, req.GetName())
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

	vmServer := newComputeVMServer()
	sdk.Serve(&fakeComputeModule{manifest: mf.ToProto()}, sdk.FunctionProvider{
		Name:     "compute.vm/v1",
		Register: func(s *grpc.Server) { computevmv1.RegisterComputeVMServer(s, vmServer) },
	})
}
