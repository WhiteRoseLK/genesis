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
	"net/url"
	"os"
	"path"
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
	Endpoint        string `json:"endpoint"`
	Node            string `json:"node"`
	Storage         string `json:"storage"`
	Bridge          string `json:"bridge"`
	Image           string `json:"image"`
	BaseTemplate    string `json:"base_template"`
	CloudImageURL   string `json:"cloud_image_url"`
	DownloadStorage string `json:"download_storage"`
	VMIDMin         int    `json:"vmid_min"`
	FullClone       *bool  `json:"full_clone"`
	KeepTemplate    bool   `json:"keep_template"`
	TLSInsecure     bool   `json:"tls_insecure"`
	TLSCACert       string `json:"tls_ca_cert"`
	TLSCACertRef    string `json:"tls_ca_cert_ref"`
	Credentials     struct {
		TokenID     string `json:"token_id"`
		TokenSecret string `json:"token_secret"`
	} `json:"credentials"`
}

var protectedVMIDs = map[int]bool{
	100:   true,
	112:   true,
	113:   true,
	200:   true,
	202:   true,
	300:   true,
	800:   true,
	900:   true,
	998:   true,
	999:   true,
	10000: true,
	10001: true,
}

func isProtectedVMID(vmid int) bool {
	return protectedVMIDs[vmid]
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

	mu              sync.Mutex
	client          *proxmoxapi.Client
	config          *proxmoxConfig
	managedTemplate bool
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
	client, cfg, err := m.ensureClient(req.GetConfig())
	if err != nil {
		return nil, err
	}
	if _, err := client.Version(ctx); err != nil {
		return nil, fmt.Errorf("connecting to Proxmox: %w", err)
	}

	imageName := cfg.Image
	if imageName != "" {
		tmpl, err := client.FindTemplateByName(ctx, cfg.Node, imageName)
		if err != nil {
			return nil, fmt.Errorf("checking template %q: %w", imageName, err)
		}
		if tmpl == nil {
			return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_TODO}, nil
		}
	}

	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
}

func (m *proxmoxModule) stepOK(req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: req.GetState()}, nil
}

func (m *proxmoxModule) Provision(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	client, cfg, err := m.ensureClient(req.GetConfig())
	if err != nil {
		return nil, err
	}

	state := sdk.StateMap(req.GetState())

	imageName := cfg.Image
	var tmpl *proxmoxapi.VM
	if imageName != "" {
		existing, err := client.FindTemplateByName(ctx, cfg.Node, imageName)
		if err != nil {
			return nil, fmt.Errorf("checking template %q: %w", imageName, err)
		}
		if existing == nil {
			s := &computeVMServer{module: m}
			newTmpl, err := s.ensureTemplate(ctx, client, cfg, imageName)
			if err != nil {
				return nil, fmt.Errorf("provisioning template %q: %w", imageName, err)
			}
			tmpl = newTmpl
			state["template_managed"] = true
			m.mu.Lock()
			m.managedTemplate = true
			m.mu.Unlock()
		} else {
			tmpl = existing
			if isManaged, _ := state["template_managed"].(bool); !isManaged {
				vmCfg, err := client.GetVMConfig(ctx, cfg.Node, tmpl.VMID)
				if err == nil {
					if desc, ok := vmCfg["description"].(string); ok && strings.Contains(desc, "Genesis Base Template") {
						state["template_managed"] = true
					}
				}
				if tmpl.Name == "genesis-template" {
					state["template_managed"] = true
				}
			}
		}
	}

	if tmpl != nil {
		state["template_name"] = tmpl.Name
		state["template_vmid"] = int64(tmpl.VMID)
	}

	newState, err := sdk.NewState(state)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: newState}, nil
}

func (m *proxmoxModule) Configure(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return m.stepOK(req)
}

func (m *proxmoxModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	client, cfg, err := m.ensureClient(req.GetConfig())
	if err != nil {
		return nil, err
	}
	if cfg.Image != "" {
		tmpl, err := client.FindTemplateByName(ctx, cfg.Node, cfg.Image)
		if err != nil {
			return nil, fmt.Errorf("verifying template %q: %w", cfg.Image, err)
		}
		if tmpl == nil {
			return nil, fmt.Errorf("template %q not found on node %q", cfg.Image, cfg.Node)
		}
	}
	return m.stepOK(req)
}

func (m *proxmoxModule) Destroy(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	client, cfg, err := m.ensureClient(req.GetConfig())
	if err != nil {
		return nil, err
	}

	if cfg.KeepTemplate {
		return m.stepOK(req)
	}

	state := sdk.StateMap(req.GetState())
	isManaged, _ := state["template_managed"].(bool)
	m.mu.Lock()
	if m.managedTemplate {
		isManaged = true
	}
	m.mu.Unlock()

	templateName, _ := state["template_name"].(string)
	if templateName == "" {
		templateName = cfg.Image
	}

	var targetVMID int
	if vmidVal, ok := state["template_vmid"]; ok {
		switch v := vmidVal.(type) {
		case float64:
			targetVMID = int(v)
		case int64:
			targetVMID = int(v)
		case int:
			targetVMID = v
		}
	}

	var tmpl *proxmoxapi.VM
	if targetVMID > 0 {
		vms, err := client.ListVMs(ctx, cfg.Node)
		if err == nil {
			for _, vm := range vms {
				if vm.VMID == targetVMID && vm.Template == 1 {
					t := vm
					tmpl = &t
					break
				}
			}
		}
	}
	if tmpl == nil && templateName != "" {
		tmpl, _ = client.FindTemplateByName(ctx, cfg.Node, templateName)
	}

	if tmpl != nil {
		if !isManaged {
			vmCfg, err := client.GetVMConfig(ctx, cfg.Node, tmpl.VMID)
			if err == nil {
				if desc, ok := vmCfg["description"].(string); ok && strings.Contains(desc, "Genesis Base Template") {
					isManaged = true
				}
			}
			if tmpl.Name == "genesis-template" {
				isManaged = true
			}
		}

		if isManaged {
			if isProtectedVMID(tmpl.VMID) {
				return nil, fmt.Errorf("refusing to delete protected VM/template ID %d", tmpl.VMID)
			}
			if err := client.DeleteVM(ctx, cfg.Node, tmpl.VMID); err != nil {
				return nil, fmt.Errorf("deleting template %q (ID %d): %w", tmpl.Name, tmpl.VMID, err)
			}
		}
	}

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
	imageName := req.GetImage()
	if imageName == "" {
		imageName = cfg.Image
	}
	if _, err := s.ensureTemplate(ctx, client, cfg, imageName); err != nil {
		return nil, err
	}
	return &computevmv1.EnsureImageResponse{}, nil
}

const defaultCloudImageURL = "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"

func (s *computeVMServer) ensureTemplate(ctx context.Context, client *proxmoxapi.Client, cfg *proxmoxConfig, imageName string) (*proxmoxapi.VM, error) {
	tmpl, err := client.FindTemplateByName(ctx, cfg.Node, imageName)
	if err != nil {
		return nil, err
	}
	if tmpl != nil {
		return tmpl, nil
	}

	s.module.mu.Lock()
	s.module.managedTemplate = true
	s.module.mu.Unlock()

	// 1. Fallback: if base_template is explicitly configured and found on the node, clone from it.
	if cfg.BaseTemplate != "" {
		baseTmpl, err := s.findBaseTemplate(ctx, client, cfg)
		if err == nil && baseTmpl != nil {
			return s.createTemplateFromBase(ctx, client, cfg, imageName, baseTmpl)
		}
	}

	// 2. Self-bootstrapping on a virgin cluster:
	// Automatically download official cloud image and create golden template directly via Proxmox REST API.
	return s.buildTemplateFromCloudImage(ctx, client, cfg, imageName)
}

func (s *computeVMServer) buildTemplateFromCloudImage(ctx context.Context, client *proxmoxapi.Client, cfg *proxmoxConfig, imageName string) (*proxmoxapi.VM, error) {
	imageURL := cfg.CloudImageURL
	if imageURL == "" {
		imageURL = defaultCloudImageURL
	}

	downloadStorage := cfg.DownloadStorage
	if downloadStorage == "" {
		downloadStorage = "local"
	}

	targetStorage := cfg.Storage
	if targetStorage == "" {
		targetStorage = "SSD"
	}

	bridge := cfg.Bridge
	if bridge == "" {
		bridge = "vmbr0"
	}

	// Ensure the download storage is configured to accept 'import' content
	if err := client.EnsureStorageContent(ctx, downloadStorage, "import"); err != nil {
		return nil, fmt.Errorf("ensuring import content on storage %q: %w", downloadStorage, err)
	}

	// Derive filename (e.g. noble-server-cloudimg-amd64.qcow2)
	base := path.Base(imageURL)
	filename := strings.TrimSuffix(base, ".img")
	if !strings.HasSuffix(filename, ".qcow2") && !strings.HasSuffix(filename, ".raw") {
		filename += ".qcow2"
	}

	volID := fmt.Sprintf("%s:import/%s", downloadStorage, filename)
	existingVol, err := client.FindStorageVolume(ctx, cfg.Node, downloadStorage, volID)
	if err != nil || existingVol == nil {
		// Download the cloud image directly into Proxmox storage
		upid, err := client.DownloadURL(ctx, cfg.Node, downloadStorage, imageURL, filename, "import")
		if err != nil {
			return nil, fmt.Errorf("downloading cloud image %s to %s: %w", imageURL, volID, err)
		}
		if err := client.WaitForTask(ctx, cfg.Node, upid); err != nil {
			return nil, fmt.Errorf("waiting for cloud image download %s: %w", imageURL, err)
		}
	}

	// Choose template ID: try cfg.VMIDMin (e.g. 3000) or find next available ID
	templateID := cfg.VMIDMin
	if templateID <= 0 {
		templateID = 3000
	}
	vms, err := client.ListVMs(ctx, cfg.Node)
	if err != nil {
		return nil, err
	}
	for _, vm := range vms {
		if vm.Name == imageName && vm.Template == 1 {
			return &vm, nil
		}
		if vm.VMID == templateID {
			templateID, err = client.NextID(ctx, cfg.VMIDMin)
			if err != nil {
				return nil, fmt.Errorf("getting next VM ID for template: %w", err)
			}
			break
		}
	}

	// Create VM with imported cloud disk
	createOpts := proxmoxapi.CreateVMOptions{
		VMID:         templateID,
		Name:         imageName,
		Memory:       2048,
		Cores:        2,
		CPU:          "host",
		SCSIHardware: "virtio-scsi-single",
		SCSI0:        fmt.Sprintf("%s:0,import-from=%s", targetStorage, volID),
		Net0:         fmt.Sprintf("virtio,bridge=%s", bridge),
		IDE2:         fmt.Sprintf("%s:cloudinit", targetStorage),
		Boot:         "order=scsi0",
		Serial0:      "socket",
		VGA:          "serial0",
		Agent:        "1",
		Description:  "Genesis Base Template",
	}
	if err := client.CreateVM(ctx, cfg.Node, createOpts); err != nil {
		return nil, fmt.Errorf("creating template VM %d (%s): %w", templateID, imageName, err)
	}

	// Resize scsi0 to 10G
	_ = client.ResizeDisk(ctx, cfg.Node, templateID, "scsi0", "10G")

	// Convert to template
	if err := client.ConvertToTemplate(ctx, cfg.Node, templateID); err != nil {
		return nil, fmt.Errorf("converting VM %d (%s) to template: %w", templateID, imageName, err)
	}

	return client.FindTemplateByName(ctx, cfg.Node, imageName)
}

func (s *computeVMServer) createTemplateFromBase(ctx context.Context, client *proxmoxapi.Client, cfg *proxmoxConfig, imageName string, baseTmpl *proxmoxapi.VM) (*proxmoxapi.VM, error) {
	templateID := cfg.VMIDMin
	if templateID <= 0 {
		templateID = 3000
	}
	vms, err := client.ListVMs(ctx, cfg.Node)
	if err != nil {
		return nil, err
	}
	for _, vm := range vms {
		if vm.Name == imageName && vm.Template == 1 {
			return &vm, nil
		}
		if vm.VMID == templateID {
			templateID, err = client.NextID(ctx, cfg.VMIDMin)
			if err != nil {
				return nil, fmt.Errorf("getting next VM ID for template: %w", err)
			}
			break
		}
	}

	opts := proxmoxapi.CloneVMOptions{
		TemplateID: baseTmpl.VMID,
		NewID:      templateID,
		Name:       imageName,
		Full:       true,
		Storage:    cfg.Storage,
	}
	if err := client.CloneVM(ctx, cfg.Node, opts); err != nil {
		return nil, fmt.Errorf("cloning base template %q (ID %d) to create template %q (ID %d): %w",
			baseTmpl.Name, baseTmpl.VMID, imageName, templateID, err)
	}

	configParams := url.Values{}
	if cfg.Storage != "" {
		configParams.Set("ide2", fmt.Sprintf("%s:cloudinit", cfg.Storage))
	}
	configParams.Set("serial0", "socket")
	configParams.Set("vga", "serial0")
	configParams.Set("description", "Genesis Base Template")
	_ = client.ConfigureVM(ctx, cfg.Node, templateID, configParams)

	if err := s.sanitizeTemplate(ctx, client, cfg.Node, templateID); err != nil {
		_ = client.DeleteVM(ctx, cfg.Node, templateID)
		return nil, fmt.Errorf("sanitizing template VM %d (%s): %w", templateID, imageName, err)
	}

	if err := client.ConvertToTemplate(ctx, cfg.Node, templateID); err != nil {
		return nil, fmt.Errorf("converting VM %d (%s) to template: %w", templateID, imageName, err)
	}

	return client.FindTemplateByName(ctx, cfg.Node, imageName)
}

const sanitizeTemplateScript = `sed -i 's/GRUB_CMDLINE_LINUX_DEFAULT=.*/GRUB_CMDLINE_LINUX_DEFAULT="quiet"/' /etc/default/grub && update-grub && rm -f /etc/cloud/cloud-init.disabled /etc/netplan/* && cloud-init clean --logs --seed && truncate -s 0 /etc/machine-id /var/lib/dbus/machine-id && rm -f /etc/ssh/ssh_host_* && sync`

func (s *computeVMServer) sanitizeTemplate(ctx context.Context, client *proxmoxapi.Client, node string, vmid int) error {
	// Start VM so the QEMU Guest Agent becomes available
	if err := client.StartVM(ctx, node, vmid); err != nil {
		return fmt.Errorf("starting VM for sanitization: %w", err)
	}

	// Wait up to 3 minutes for QEMU Guest Agent to respond
	agentCtx, cancelAgent := context.WithTimeout(ctx, 3*time.Minute)
	defer cancelAgent()
	if err := client.WaitForAgent(agentCtx, node, vmid); err != nil {
		_ = client.StopVM(ctx, node, vmid)
		return fmt.Errorf("waiting for guest agent: %w", err)
	}

	// Run sanitization command
	execCtx, cancelExec := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelExec()
	if _, err := client.AgentExec(execCtx, node, vmid, []string{"sh", "-c", sanitizeTemplateScript}); err != nil {
		_ = client.StopVM(ctx, node, vmid)
		return fmt.Errorf("executing sanitization script: %w", err)
	}

	// Stop VM and wait until fully stopped before converting to template
	if err := client.StopVM(ctx, node, vmid); err != nil {
		return fmt.Errorf("stopping VM after sanitization: %w", err)
	}
	stopCtx, cancelStop := context.WithTimeout(ctx, 1*time.Minute)
	defer cancelStop()
	if err := client.WaitForStopped(stopCtx, node, vmid); err != nil {
		return fmt.Errorf("waiting for VM to stop: %w", err)
	}

	return nil
}

func (s *computeVMServer) findBaseTemplate(ctx context.Context, client *proxmoxapi.Client, cfg *proxmoxConfig) (*proxmoxapi.VM, error) {
	vms, err := client.ListVMs(ctx, cfg.Node)
	if err != nil {
		return nil, err
	}

	// 1. If BaseTemplate is explicitly configured, match by name or VMID
	if cfg.BaseTemplate != "" {
		for i := range vms {
			if vms[i].Template == 1 && (vms[i].Name == cfg.BaseTemplate || strconv.Itoa(vms[i].VMID) == cfg.BaseTemplate) {
				return &vms[i], nil
			}
		}
		return nil, fmt.Errorf("base_template %q not found as a template on node %q", cfg.BaseTemplate, cfg.Node)
	}

	// 2. Search for any template whose name contains "ubuntu" (case-insensitive)
	for i := range vms {
		if vms[i].Template == 1 && strings.Contains(strings.ToLower(vms[i].Name), "ubuntu") {
			return &vms[i], nil
		}
	}

	// 3. Search for any template whose name contains "debian" (case-insensitive)
	for i := range vms {
		if vms[i].Template == 1 && strings.Contains(strings.ToLower(vms[i].Name), "debian") {
			return &vms[i], nil
		}
	}

	// 4. Fallback to any template on the node
	for i := range vms {
		if vms[i].Template == 1 {
			return &vms[i], nil
		}
	}

	return nil, fmt.Errorf("no base template found on node %q: please ensure a cloud-init template is available on Proxmox VE", cfg.Node)
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

	template, err := s.ensureTemplate(ctx, client, cfg, cfg.Image)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, fmt.Errorf("template %q not found", cfg.Image)
	}

	fullClone := false
	if cfg.FullClone != nil {
		fullClone = *cfg.FullClone
	}
	// Ephemeral or test VMs (e.g. verifiers, test targets) always use linked clones
	// for fast provisioning (<2s vs ~75s full clone) and minimal storage overhead,
	// while final production VMs keep the configured full_clone setting.
	if isEphemeralVM(req) {
		fullClone = false
	}

	var newID int
	var upid string

	// Allocate VM ID and initiate the clone under lock so concurrent provisioning
	// requests receive strictly distinct IDs before asynchronous cloning starts.
	s.module.mu.Lock()
	if cfg.VMIDMin > 0 {
		newID, err = client.NextID(ctx, cfg.VMIDMin)
	} else {
		newID, err = client.NextID(ctx)
	}
	if err != nil {
		s.module.mu.Unlock()
		return nil, fmt.Errorf("getting a VM ID: %w", err)
	}

	upid, err = client.InitiateCloneVM(ctx, cfg.Node, proxmoxapi.CloneVMOptions{
		TemplateID: template.VMID,
		NewID:      newID,
		Name:       req.GetName(),
		Full:       fullClone,
		Storage:    cfg.Storage,
	})
	s.module.mu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("cloning %q: %w", req.GetName(), err)
	}

	// Wait for the asynchronous clone task outside the lock so multiple VM
	// clones proceed concurrently on Proxmox.
	if upid != "" {
		if err := client.WaitForTask(ctx, cfg.Node, upid); err != nil {
			return nil, fmt.Errorf("waiting for clone of %q: %w", req.GetName(), err)
		}
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

// isEphemeralVM determines if a VM request is for an ephemeral test or verification VM
// that should use linked clones instead of full clones.
func isEphemeralVM(req *computevmv1.EnsureVMRequest) bool {
	if req.GetExtra() != nil {
		if val, ok := req.GetExtra()["full_clone"]; ok {
			return val == "false" || val == "0"
		}
		if val, ok := req.GetExtra()["ephemeral"]; ok {
			return val == "true" || val == "1"
		}
	}
	name := req.GetName()
	return strings.HasSuffix(name, "-verify") || strings.HasSuffix(name, "-agent-target")
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
