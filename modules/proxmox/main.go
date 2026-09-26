// SPDX-License-Identifier: Apache-2.0

// proxmox fournit compute.vm/v1 sur un cluster Proxmox VE existant
// (docs/07-modules-mvp.md). Pas d'accès à un vrai cluster dans cet
// environnement de développement (voir docs/PROGRESS.md, J5) : construit et
// testé contre des fixtures HTTP, jamais exécuté contre une instance réelle.
//
// Portée assumée pour ce jalon : EnsureImage suppose qu'un template existe
// déjà sous le nom demandé (`image` dans la config) plutôt que de
// télécharger l'image cloud et de créer le template automatiquement — cette
// chaîne d'opérations est la plus complexe et la moins vérifiable sans
// cluster réel ; à construire quand elle pourra être validée (J9 ou sur
// demande explicite).
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
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
	Endpoint    string `json:"endpoint"`
	Node        string `json:"node"`
	Storage     string `json:"storage"`
	Bridge      string `json:"bridge"`
	Image       string `json:"image"`
	Credentials struct {
		TokenID     string `json:"token_id"`
		TokenSecret string `json:"token_secret"`
	} `json:"credentials"`
}

func parseConfig(s *structpb.Struct) (*proxmoxConfig, error) {
	raw, err := json.Marshal(s.AsMap())
	if err != nil {
		return nil, fmt.Errorf("encodage de la config : %w", err)
	}
	var cfg proxmoxConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("décodage de la config : %w", err)
	}
	if cfg.Endpoint == "" || cfg.Node == "" {
		return nil, fmt.Errorf("config proxmox incomplète : endpoint et node sont requis")
	}
	return &cfg, nil
}

// proxmoxModule porte le cycle de vie. Sa seule responsabilité propre est de
// vérifier la connexion à l'API ; le travail réel se fait dans
// computeVMServer en réponse aux appels de compute.vm/v1.
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

// ensureClient construit (et met en cache) le client Proxmox à partir de la
// config résolue — réutilisé ensuite par computeVMServer, qui n'a pas
// d'accès direct à StepRequest.config (docs/02-architecture.md : la config
// n'arrive qu'aux étapes du cycle de vie, pas aux appels de fonction).
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
	m.client = proxmoxapi.New(cfg.Endpoint, cfg.Credentials.TokenID, cfg.Credentials.TokenSecret, nil)
	m.config = cfg
	return m.client, m.config, nil
}

func (m *proxmoxModule) Check(ctx context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	client, _, err := m.ensureClient(req.GetConfig())
	if err != nil {
		return nil, err
	}
	if _, err := client.Version(ctx); err != nil {
		return nil, fmt.Errorf("connexion à Proxmox : %w", err)
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_CONFORME}, nil
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

// computeVMServer implémente la fonction compute.vm/v1.
type computeVMServer struct {
	computevmv1.UnimplementedComputeVMServer
	module *proxmoxModule
}

func (s *computeVMServer) client() (*proxmoxapi.Client, *proxmoxConfig, error) {
	s.module.mu.Lock()
	defer s.module.mu.Unlock()
	if s.module.client == nil {
		return nil, nil, fmt.Errorf("proxmox : aucune configuration (Check n'a pas encore été appelé sur ce module)")
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
			"template %q introuvable sur le nœud %q : la création automatique du template (téléchargement de l'image cloud) n'est pas encore prise en charge à ce jalon — créez-le manuellement au préalable",
			req.GetImage(), cfg.Node,
		)
	}
	return &computevmv1.EnsureImageResponse{}, nil
}

// EnsureVM est idempotent par nom (clé d'idempotence, docs/07).
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
		return &computevmv1.VM{Id: strconv.Itoa(existing.VMID), Name: existing.Name, Ip: req.GetIp(), Status: status.Status}, nil
	}

	template, err := client.FindTemplateByName(ctx, cfg.Node, cfg.Image)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, fmt.Errorf("template %q introuvable : appelez EnsureImage au préalable", cfg.Image)
	}

	newID, err := client.NextID(ctx)
	if err != nil {
		return nil, fmt.Errorf("obtention d'un identifiant de VM : %w", err)
	}

	if err := client.CloneVM(ctx, cfg.Node, proxmoxapi.CloneVMOptions{
		TemplateID: template.VMID, NewID: newID, Name: req.GetName(),
	}); err != nil {
		return nil, fmt.Errorf("clonage de %q : %w", req.GetName(), err)
	}

	if err := client.ConfigureCloudInit(ctx, cfg.Node, newID, proxmoxapi.CloudInitOptions{
		User:         req.GetUser(),
		SSHPublicKey: req.GetSshPublicKey(),
		IP:           req.GetIp(),
		Gateway:      req.GetGateway(),
		Tags:         []string{"genesis-env=" + req.GetEnv()},
	}); err != nil {
		return nil, fmt.Errorf("configuration cloud-init de %q : %w", req.GetName(), err)
	}

	if err := client.StartVM(ctx, cfg.Node, newID); err != nil {
		return nil, fmt.Errorf("démarrage de %q : %w", req.GetName(), err)
	}

	status, err := client.Status(ctx, cfg.Node, newID)
	if err != nil {
		return nil, err
	}
	return &computevmv1.VM{Id: strconv.Itoa(newID), Name: req.GetName(), Ip: req.GetIp(), Status: status.Status}, nil
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
		return nil, fmt.Errorf("VM %q introuvable", req.GetName())
	}
	status, err := client.Status(ctx, cfg.Node, vm.VMID)
	if err != nil {
		return nil, err
	}
	return &computevmv1.VM{Id: strconv.Itoa(vm.VMID), Name: vm.Name, Status: status.Status}, nil
}

// DeleteVM est idempotent : une VM déjà absente n'est pas une erreur.
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

// Now interroge l'heure du nœud (docs/05-cycle-bootstrap.md : contrôle
// d'horloge de la graine, première dépendance de toute la chaîne).
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
