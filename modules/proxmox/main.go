// SPDX-License-Identifier: Apache-2.0

// proxmox provides compute.vm/v1 on an existing Proxmox VE cluster
// (docs/07-mvp-modules.md).
//
// Template requirements: EnsureImage verifies that a Cloud-Init enabled
// template already exists under the requested name (`image` in the config).
// Automatic template download over the Proxmox REST API is not natively
// supported without host shell access; templates must be created beforehand
// (see docs/user-guide.md or scripts/create-proxmox-template.sh).
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"

	"github.com/WhiteRoseLK/genesis/modules/proxmox/proxmoxapi"
)

//go:embed module.yaml
var manifestYAML []byte

type proxmoxConfig struct {
	Endpoint     string `json:"endpoint"`
	Node         string `json:"node"`
	Storage      string `json:"storage"`
	Bridge       string `json:"bridge"`
	Image        string `json:"image"`
	VMIDMin      int    `json:"vmid_min"`
	FullClone    *bool  `json:"full_clone"`
	TLSInsecure  bool   `json:"tls_insecure"`
	TLSCACert    string `json:"tls_ca_cert"`
	TLSCACertRef string `json:"tls_ca_cert_ref"`
	Credentials  struct {
		TokenID     string `json:"token_id"`
		TokenSecret string `json:"token_secret"`
	} `json:"credentials"`
}

func parseConfig(s *structpb.Struct) (*proxmoxConfig, error) {
	raw, err := json.Marshal(s.AsMap())
	if err != nil {
		return nil, fmt.Errorf("encoding the config: %w", err)
	}
	var cfg proxmoxConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("decoding the config: %w", err)
	}
	if cfg.Endpoint == "" || cfg.Node == "" {
		return nil, fmt.Errorf("incomplete proxmox config: endpoint and node are required")
	}
	return &cfg, nil
}

// proxmoxModule carries the lifecycle. Its only own responsibility is to check
// the API connection; the real work happens in computeVMServer in response to
// compute.vm/v1 calls.
type proxmoxModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest

	mu     sync.Mutex
	client *proxmoxapi.Client
	config *proxmoxConfig
}

func (m *proxmoxModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *proxmoxModule) Validate(_ context.Context, req *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	if _, err := parseConfig(req.GetConfig()); err != nil {
		return &modulev1.Diagnostics{Diagnostics: []*modulev1.Diagnostic{{Message: err.Error(), Severity: "error"}}}, nil
	}
	return &modulev1.Diagnostics{}, nil
}

// ensureClient builds (and caches) the Proxmox client from the resolved config
// — reused afterwards by computeVMServer, which has no direct access to
// StepRequest.config (docs/02-architecture.md: the config only reaches
// lifecycle steps, not function calls).
func (m *proxmoxModule) ensureClient(cfgStruct *structpb.Struct) (*proxmoxapi.Client, *proxmoxConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		return m.client, m.config, nil
	}
	cfg, err := parseConfig(cfgStruct)
	if err != nil {
		return nil, nil, err
	}

	caPEM := []byte(cfg.TLSCACert)
	if len(caPEM) == 0 && cfg.TLSCACertRef != "" {
		if strings.HasPrefix(cfg.TLSCACertRef, "file://") {
			path := strings.TrimPrefix(cfg.TLSCACertRef, "file://")
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, nil, fmt.Errorf("reading CA cert file %s: %w", path, err)
			}
			caPEM = data
		} else if strings.HasPrefix(cfg.TLSCACertRef, "env://") {
			name := strings.TrimPrefix(cfg.TLSCACertRef, "env://")
			val, ok := os.LookupEnv(name)
			if !ok {
				return nil, nil, fmt.Errorf("CA cert environment variable %s is not set", name)
			}
			caPEM = []byte(val)
		}
	}

	httpClient, err := proxmoxapi.NewHTTPClient(proxmoxapi.TLSOptions{
		Insecure:  cfg.TLSInsecure,
		CACertPEM: caPEM,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configuring Proxmox TLS client: %w", err)
	}

	m.client = proxmoxapi.New(cfg.Endpoint, cfg.Credentials.TokenID, cfg.Credentials.TokenSecret, httpClient)
	m.config = cfg
	return m.client, m.config, nil
}

func (m *proxmoxModule) Check(ctx context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	client, _, err := m.ensureClient(req.GetConfig())
	if err != nil {
		return nil, err
	}
	if _, err := client.Version(ctx); err != nil {
		return nil, fmt.Errorf("connecting to Proxmox: %w", err)
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
}

func (m *proxmoxModule) stepOK(req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

func (m *proxmoxModule) Provision(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return m.stepOK(req)
}

func (m *proxmoxModule) Configure(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return m.stepOK(req)
}

func (m *proxmoxModule) Verify(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return m.stepOK(req)
}

func (m *proxmoxModule) Destroy(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return m.stepOK(req)
}

// computeVMServer implements the compute.vm/v1 function.
type computeVMServer struct {
	computevmv1.UnimplementedComputeVMServer
	module *proxmoxModule
}

func (s *computeVMServer) client() (*proxmoxapi.Client, *proxmoxConfig, error) {
	s.module.mu.Lock()
	defer s.module.mu.Unlock()
	if s.module.client == nil {
		return nil, nil, fmt.Errorf("proxmox: no configuration (Check has not been called on this module yet)")
	}
	return s.module.client, s.module.config, nil
}

func (s *computeVMServer) EnsureImage(ctx context.Context, req *computevmv1.EnsureImageRequest) (*computevmv1.EnsureImageResponse, error) {
	client, cfg, err := s.client()
	if err != nil {
		return nil, err
	}
	tmpl, err := client.FindTemplateByName(ctx, cfg.Node, req.GetImage())
	if err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, fmt.Errorf(
			"template %q not found on node %q: Proxmox VE templates must be prepared beforehand with cloud-init (see docs/user-guide.md or scripts/create-proxmox-template.sh)",
			req.GetImage(), cfg.Node,
		)
	}
	return &computevmv1.EnsureImageResponse{}, nil
}

// EnsureVM is idempotent by name (idempotence key, docs/07).
func (s *computeVMServer) EnsureVM(ctx context.Context, req *computevmv1.EnsureVMRequest) (*computevmv1.VM, error) {
	client, cfg, err := s.client()
	if err != nil {
		return nil, err
	}

	if existing, err := client.FindVMByName(ctx, cfg.Node, req.GetName()); err != nil {
		return nil, err
	} else if existing != nil {
		status, err := client.Status(ctx, cfg.Node, existing.VMID)
		if err != nil {
			return nil, err
		}
		vmIP := req.GetIp()
		if idx := strings.IndexByte(vmIP, '/'); idx != -1 {
			vmIP = vmIP[:idx]
		}
		return &computevmv1.VM{Id: strconv.Itoa(existing.VMID), Name: existing.Name, Ip: vmIP, Status: status.Status, SshPort: sshPort, SshHostPublicKey: req.GetSshHostPublicKey()}, nil
	}

	template, err := client.FindTemplateByName(ctx, cfg.Node, cfg.Image)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, fmt.Errorf("template %q not found: call EnsureImage first", cfg.Image)
	}

	var newID int
	if cfg.VMIDMin > 0 {
		newID, err = client.NextID(ctx, cfg.VMIDMin)
	} else {
		newID, err = client.NextID(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("getting a VM ID: %w", err)
	}

	fullClone := false
	if cfg.FullClone != nil {
		fullClone = *cfg.FullClone
	}
	if err := client.CloneVM(ctx, cfg.Node, proxmoxapi.CloneVMOptions{
		TemplateID: template.VMID,
		NewID:      newID,
		Name:       req.GetName(),
		Full:       fullClone,
		Storage:    cfg.Storage,
	}); err != nil {
		return nil, fmt.Errorf("cloning %q: %w", req.GetName(), err)
	}

	tag := "genesis"
	if env := req.GetEnv(); env != "" {
		tag = "genesis-env-" + env
	}
	if err := client.ConfigureCloudInit(ctx, cfg.Node, newID, proxmoxapi.CloudInitOptions{
		User:         req.GetUser(),
		SSHPublicKey: req.GetSshPublicKey(),
		IP:           req.GetIp(),
		Gateway:      req.GetGateway(),
		Tags:         []string{tag},
	}); err != nil {
		return nil, fmt.Errorf("cloud-init configuration of %q: %w", req.GetName(), err)
	}

	if err := client.StartVM(ctx, cfg.Node, newID); err != nil {
		return nil, fmt.Errorf("starting %q: %w", req.GetName(), err)
	}

	status, err := client.Status(ctx, cfg.Node, newID)
	if err != nil {
		return nil, err
	}
	vmIP := req.GetIp()
	if idx := strings.IndexByte(vmIP, '/'); idx != -1 {
		vmIP = vmIP[:idx]
	}

	// Wait for SSH to become reachable on the newly started VM (docs/07-mvp-modules.md).
	// In integration tests with mock httptest servers, there is no real VM behind the IP, so skip.
	if vmIP != "" && vmIP != "127.0.0.1" && !strings.Contains(cfg.Endpoint, "127.0.0.1") && !strings.Contains(cfg.Endpoint, "localhost") {
		if err := waitForSSH(ctx, vmIP, sshPort, 120*time.Second); err != nil {
			return nil, fmt.Errorf("waiting for VM %q SSH: %w", req.GetName(), err)
		}
		// Brief pause to allow cloud-init to finalize authorized_keys and sshd initialization
		time.Sleep(3 * time.Second)
	}

	return &computevmv1.VM{Id: strconv.Itoa(newID), Name: req.GetName(), Ip: vmIP, Status: status.Status, SshPort: sshPort, SshHostPublicKey: req.GetSshHostPublicKey()}, nil
}

// sshPort: a Proxmox VM is a real VM, SSH listens on the standard port
// (compute.vm/v1: consumers use VM.ssh_port, never a hard-coded 22).
const sshPort = 22

func waitForSSH(ctx context.Context, host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return fmt.Errorf("timeout waiting for SSH on %s:%d", host, port)
}

func (s *computeVMServer) GetVM(ctx context.Context, req *computevmv1.GetVMRequest) (*computevmv1.VM, error) {
	client, cfg, err := s.client()
	if err != nil {
		return nil, err
	}
	vm, err := client.FindVMByName(ctx, cfg.Node, req.GetName())
	if err != nil {
		return nil, err
	}
	if vm == nil {
		return nil, fmt.Errorf("VM %q not found", req.GetName())
	}
	status, err := client.Status(ctx, cfg.Node, vm.VMID)
	if err != nil {
		return nil, err
	}
	return &computevmv1.VM{Id: strconv.Itoa(vm.VMID), Name: vm.Name, Status: status.Status, SshPort: sshPort}, nil
}

// DeleteVM is idempotent: a VM that is already gone is not an error.
func (s *computeVMServer) DeleteVM(ctx context.Context, req *computevmv1.DeleteVMRequest) (*computevmv1.DeleteVMResponse, error) {
	client, cfg, err := s.client()
	if err != nil {
		return nil, err
	}
	vm, err := client.FindVMByName(ctx, cfg.Node, req.GetName())
	if err != nil {
		return nil, err
	}
	if vm == nil {
		return &computevmv1.DeleteVMResponse{}, nil
	}
	if err := client.DeleteVM(ctx, cfg.Node, vm.VMID); err != nil {
		return nil, err
	}
	return &computevmv1.DeleteVMResponse{}, nil
}

// Now queries the node's clock (docs/05-bootstrap-lifecycle.md: the seed's
// clock check, the first dependency of the whole chain).
func (s *computeVMServer) Now(ctx context.Context, _ *computevmv1.NowRequest) (*computevmv1.NowResponse, error) {
	client, cfg, err := s.client()
	if err != nil {
		return nil, err
	}
	t, err := client.Time(ctx, cfg.Node)
	if err != nil {
		return nil, err
	}
	return &computevmv1.NowResponse{Time: timestamppb.New(time.Unix(t.Time, 0))}, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	module := &proxmoxModule{manifest: mf.ToProto()}
	vmServer := &computeVMServer{module: module}
	sdk.Serve(module, sdk.FunctionProvider{
		Name:     "compute.vm/v1",
		Register: func(s *grpc.Server) { computevmv1.RegisterComputeVMServer(s, vmServer) },
	})
}
