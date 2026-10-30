// SPDX-License-Identifier: Apache-2.0

// fake-compute provides compute.vm/v1 through real SSH-reachable containers on
// the seed (docs/07-mvp-modules.md), to test the core and the service modules
// without a hypervisor. Each "VM" is a real lscr.io/linuxserver/openssh-server
// container, started through core.container/v1 — no systemd inside (unlike the
// letter of doc 07): the point of the module is to be a real SSH target for
// the other modules, not to faithfully reproduce a complete operating system.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"net"
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

// sshTargetImage and the internal port are fixed: the point of fake-compute is
// to be a predictable SSH target, not a configurable one
// (docs/08-milestones.md, M4/M6).
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
	// Nothing to provision for the module itself: the fake VMs are created on
	// demand through the compute.vm/v1 function. Captures the session token to
	// reach core.container/v1 (the same mechanism as modules/base-os with
	// core.ansible/v1).
	_ = m.dialWithToken(req.GetBrokerToken())
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
}

func (m *fakeComputeModule) Provision(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	_ = m.dialWithToken(req.GetBrokerToken())
	return ok(req)
}

func (m *fakeComputeModule) Configure(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	_ = m.dialWithToken(req.GetBrokerToken())
	return ok(req)
}

func (m *fakeComputeModule) Verify(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	_ = m.dialWithToken(req.GetBrokerToken())
	return ok(req)
}

func (m *fakeComputeModule) Repoint(_ context.Context, req *modulev1.RepointRequest) (*modulev1.StepResult, error) {
	if err := m.dialWithToken(req.GetStep().GetBrokerToken()); err != nil {
		return nil, err
	}
	return ok(req.GetStep())
}

func (m *fakeComputeModule) Destroy(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	_ = m.dialWithToken(req.GetBrokerToken())
	return ok(req)
}

func ok(req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	s, err := sdk.NewState(sdk.StateMap(req.GetState()))
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

// dialWithToken dials the broker session with token, or the cached token if
// token is empty. If a new token is provided, it redials so that repoints are
// observed. Dial is only attempted once per token.
func (m *fakeComputeModule) dialWithToken(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if token == "" {
		if m.containerClient != nil {
			return nil
		}
		token = m.brokerToken
	}
	if token == "" {
		return fmt.Errorf("fake-compute: no broker session (Check has not been called yet)")
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

func (m *fakeComputeModule) containers() (containerv1.ContainerClient, error) {
	if err := m.dialWithToken(""); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.containerClient, nil
}

// computeVMServer implements functions/compute/vm/v1.
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

// EnsureVM is idempotent by name (idempotence key, docs/07-mvp-modules.md).
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

	containerName := "genesis-vm-" + req.GetName()
	resp, err := containers.Run(ctx, &containerv1.RunRequest{
		Name:  containerName,
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
		// If the container already exists from a previous or interrupted run,
		// remove it and recreate it so EnsureVM is truly idempotent across restarts.
		_, _ = containers.Stop(ctx, &containerv1.StopRequest{ContainerId: containerName})
		resp, err = containers.Run(ctx, &containerv1.RunRequest{
			Name:  containerName,
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
			return nil, fmt.Errorf("starting VM %q: %w", req.GetName(), err)
		}
	}

	// A container cannot take the requested pool address: report the one it
	// actually has, so SSH and service calls reach it (#124). A loopback
	// request is kept: it means the services are published on the seed itself
	// (test/integration/e2e_fakecompute_test.go).
	ip := resp.GetIp()
	if requested := net.ParseIP(req.GetIp()); requested != nil && requested.IsLoopback() {
		ip = req.GetIp()
	}

	vm := &computevmv1.VM{
		Id:               resp.GetContainerId(),
		Name:             req.GetName(),
		Ip:               ip,
		Status:           "running",
		SshPort:          sshTargetPort,
		SshHostPublicKey: req.GetSshHostPublicKey(),
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
		return nil, fmt.Errorf("VM %q not found", req.GetName())
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
		return &computevmv1.DeleteVMResponse{}, nil // idempotent: already gone
	}

	containers, err := s.module.containers()
	if err != nil {
		return nil, err
	}
	if _, err := containers.Stop(ctx, &containerv1.StopRequest{ContainerId: vm.GetId()}); err != nil {
		return nil, fmt.Errorf("stopping VM %q: %w", req.GetName(), err)
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
