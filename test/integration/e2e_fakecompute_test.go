// SPDX-License-Identifier: Apache-2.0

//go:build docker

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/broker"
	"github.com/WhiteRoseLK/genesis/internal/engine"
	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/planner"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/spec"
	"github.com/WhiteRoseLK/genesis/internal/state"
	"github.com/WhiteRoseLK/genesis/internal/testutil"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
)

// fixedLoopbackAllocator assigns 127.0.0.1 to every simulated VM so that
// modules can reach their local services directly on the loopback interface.
type fixedLoopbackAllocator struct {
	gateway string
}

func (a *fixedLoopbackAllocator) Allocate(_ string) (string, error) {
	return "127.0.0.1", nil
}

func (a *fixedLoopbackAllocator) Release(_ string) error {
	return nil
}

func (a *fixedLoopbackAllocator) Gateway() string {
	return a.gateway
}

// cleanupGenesisContainers removes any leftover test containers with names starting with "genesis-".
func cleanupGenesisContainers() {
	cmd := exec.Command("docker", "ps", "-a", "--filter", "name=genesis-", "--format", "{{.Names}}")
	out, err := cmd.Output()
	if err == nil {
		names := strings.Fields(string(out))
		if len(names) > 0 {
			args := append([]string{"rm", "-f"}, names...)
			_ = exec.Command("docker", args...).Run()
		}
	}
}

// buildRealModules builds the 8 real modules and installs them in searchRoot.
func buildRealModules(t *testing.T, searchRoot string) []modulehost.Installed {
	t.Helper()

	moduleNames := []string{
		"fake-compute",
		"coredns",
		"step-ca",
		"base-os",
		"chrony",
		"powerdns",
		"vault",
		"teleport",
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))

	var installed []modulehost.Installed
	for _, name := range moduleNames {
		sourceDir := filepath.Join(repoRoot, "modules", name)
		manifestPath := filepath.Join(sourceDir, "module.yaml")
		manifest, err := sdk.LoadManifest(manifestPath)
		if err != nil {
			t.Fatalf("loading manifest of %s: %v", name, err)
		}

		destDir := filepath.Join(searchRoot, name, manifest.Version)
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			t.Fatalf("creating dest dir for %s: %v", name, err)
		}

		binaryPath := filepath.Join(destDir, modulehost.BinaryName())
		cmd := exec.Command("go", "build", "-o", binaryPath, ".")
		cmd.Dir = sourceDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("building %s: %v\n%s", name, err, out)
		}

		manifestContent, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("reading manifest of %s: %v", name, err)
		}
		destManifestPath := filepath.Join(destDir, "module.yaml")
		if err := os.WriteFile(destManifestPath, manifestContent, 0o644); err != nil {
			t.Fatalf("writing manifest for %s: %v", name, err)
		}

		installed = append(installed, modulehost.Installed{
			Name:         name,
			Version:      manifest.Version,
			Dir:          destDir,
			ManifestPath: destManifestPath,
			BinaryPath:   binaryPath,
			Manifest:     manifest,
		})
	}

	return installed
}

// e2eCompositeAnsibleServer routes playbooks to the appropriate simulated or container handler.
type e2eCompositeAnsibleServer struct {
	ansiblev1.UnimplementedAnsibleServer
	mu          sync.Mutex
	vaultFake   *vaultFakeAnsibleServer
	calls       int
	interrupter func(req *ansiblev1.RunPlaybookRequest) error
}

func (s *e2eCompositeAnsibleServer) RunPlaybook(ctx context.Context, req *ansiblev1.RunPlaybookRequest) (*ansiblev1.RunPlaybookResponse, error) {
	s.mu.Lock()
	s.calls++
	interrupter := s.interrupter
	s.mu.Unlock()

	if interrupter != nil {
		if err := interrupter(req); err != nil {
			return nil, err
		}
	}

	playbook := string(req.GetPlaybookYaml())

	switch {
	// Vault deploy: runs the real hashicorp/vault container with certificates from step-ca
	case strings.Contains(playbook, "install vault") || (strings.Contains(playbook, "install the prerequisites") && strings.Contains(playbook, "hashicorp")):
		return s.vaultFake.RunPlaybook(ctx, req)

	// Vault verify: tests AppRole, issuance, PKI chain, KV write/read against the real Vault container
	case strings.Contains(playbook, "install the verification tools") && strings.Contains(playbook, "AppRole"):
		return s.vaultFake.RunPlaybook(ctx, req)

	// Chrony offset check: simulates chronyc tracking output with offset < 100ms
	case strings.Contains(playbook, "wait for a usable synchronisation") || strings.Contains(playbook, "afficher chronyc tracking"):
		return &ansiblev1.RunPlaybookResponse{
			Ok: true,
			Output: `TASK [afficher chronyc tracking] ***
ok: [target] => {
    "tracking.stdout": "Reference ID    : 0A0A0005 (10.10.0.5)\nStratum         : 3\nSystem time     : 0.000021345 seconds fast of NTP time\nLeap status     : Normal"
}`,
		}, nil

	// PowerDNS verification: simulates resolution check output
	case strings.Contains(playbook, "install the DNS verification tools") || strings.Contains(playbook, "show the results"):
		return &ansiblev1.RunPlaybookResponse{
			Ok: true,
			Output: `TASK [show the results] ***
ok: [target] => {
    "msg": {
        "external": "93.184.216.34",
        "forward": "10.255.255.1",
        "reverse": "verify-probe.lab.internal."
    }
}`,
		}, nil

	// Teleport install: returns CA pin from tctl status
	case strings.Contains(playbook, "install teleport") || strings.Contains(playbook, "teleport.yaml (Auth + Proxy)") || strings.Contains(playbook, "add the Teleport repository"):
		return &ansiblev1.RunPlaybookResponse{
			Ok:     true,
			Output: "Cluster CA: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n",
		}, nil

	// Teleport node join token: returns a 32-character hex token
	case strings.Contains(playbook, "generate a node join token"):
		return &ansiblev1.RunPlaybookResponse{
			Ok:     true,
			Output: "The invite token: 0123456789abcdef0123456789abcdef\n",
		}, nil

	// Teleport user certificate generation: returns private key and cert
	case strings.Contains(playbook, "sign a short-lived SSH certificate"):
		privKey := base64.StdEncoding.EncodeToString([]byte("test-priv-key"))
		cert := base64.StdEncoding.EncodeToString([]byte("test-ssh-cert"))
		return &ansiblev1.RunPlaybookResponse{
			Ok:     true,
			Output: fmt.Sprintf("PRIVATE_KEY_B64:%s\nCERTIFICATE_B64:%s\n", privKey, cert),
		}, nil

	// Teleport SSH verification: simulates agent connect check
	case strings.Contains(playbook, "connect through the teleport agent"):
		return &ansiblev1.RunPlaybookResponse{
			Ok:     true,
			Output: "AGENT_SSH_OK\nDIRECT_SSH_REFUSED_OK\n",
		}, nil

	// Default: successful playbook run
	default:
		return &ansiblev1.RunPlaybookResponse{Ok: true, Output: "ok"}, nil
	}
}

// TestE2EMultiModuleFakeCompute verifies the end-to-end orchestration of all real modules
// on simulated VMs with fake-compute (Issue #45 / Milestone M9).
func TestE2EMultiModuleFakeCompute(t *testing.T) {
	rt := testutil.RequireRuntime(t)

	cleanupGenesisContainers()
	t.Cleanup(cleanupGenesisContainers)

	searchRoot := t.TempDir()
	installed := buildRealModules(t, searchRoot)

	env := &spec.Environment{
		APIVersion: "genesis/v1",
		Kind:       "Environment",
		Metadata: spec.Metadata{
			Name: "e2e-fakecompute",
		},
		Network: spec.Network{
			CIDR:    "10.10.0.0/16",
			Gateway: "10.10.0.1",
			Pool:    "10.10.10.0/24",
			Domain:  "lab.internal",
		},
		Seed: spec.Seed{
			Address: "10.10.0.2",
		},
		Capabilities: map[string]spec.Capability{
			"compute": {Module: "fake-compute"},
			"time":    {Module: "chrony"},
			"dns":     {Module: "powerdns"},
			"secrets": {Module: "vault"},
			"pki":     {Module: "vault"},
			"access":  {Module: "teleport"},
		},
	}

	resolved, err := resolver.Resolve(env, installed)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Verify all 8 modules are present (requested + auto-added: base-os, coredns, step-ca).
	expectedModules := []string{"fake-compute", "coredns", "step-ca", "base-os", "chrony", "powerdns", "vault", "teleport"}
	for _, m := range expectedModules {
		if _, ok := resolved.Modules[m]; !ok {
			t.Fatalf("expected module %q in resolved modules", m)
		}
	}

	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("planner.Build: %v", err)
	}

	// Start PowerDNS mock REST API server on 127.0.0.1:8081.
	startFakePowerDNSAPI(t)

	alloc := &fixedLoopbackAllocator{gateway: "127.0.0.1"}

	// --- Phase 1: Full Apply (Seed -> Target -> Handover -> Secrets Migration -> Seed Retirement) ---
	t.Run("FullApply", func(t *testing.T) {
		stateDir := t.TempDir()
		secretsStore := newTestSecretsStore(t)

		vaultAnsible := &vaultFakeAnsibleServer{t: t}
		t.Cleanup(vaultAnsible.cleanup)

		ansibleServer := &e2eCompositeAnsibleServer{vaultFake: vaultAnsible}

		var logBuf bytes.Buffer
		e := engine.New(stateDir, secretsStore)
		e.Logger = slog.New(slog.NewTextHandler(&logBuf, nil))
		e.Allocator = alloc
		e.Registry.SetNative("core.container/v1", broker.NativeContainer(rt))
		e.Registry.SetNative("core.ansible/v1", broker.NativeAnsibleServer(ansibleServer))

		ctx := context.Background()
		if err := e.Run(ctx, resolved, plan); err != nil {
			t.Fatalf("engine.Run (Phase 1): %v\nLogs:\n%s", err, logBuf.String())
		}

		st, err := state.Load(stateDir)
		if err != nil {
			t.Fatalf("state.Load: %v", err)
		}

		// 1. apply goes all the way to seed retirement:
		if !st.SeedRetired {
			t.Errorf("expected SeedRetired to be true after apply, got false")
		}

		// 2. real migration of the secrets to Vault:
		if st.SecretsBackend != "vault" {
			t.Errorf("expected SecretsBackend to be 'vault', got %q", st.SecretsBackend)
		}

		// Verify state recorded for all modules:
		for _, name := range expectedModules {
			if _, ok := st.Modules[name]; !ok {
				t.Errorf("expected module %q in saved state", name)
			}
		}

		logStr := logBuf.String()
		if !strings.Contains(logStr, "module=coredns step=seed.retire outcome=ok") {
			t.Errorf("expected coredns to have seed.retire:\n%s", logStr)
		}
		if !strings.Contains(logStr, "module=step-ca step=seed.retire outcome=ok") {
			t.Errorf("expected step-ca to have seed.retire:\n%s", logStr)
		}
		if !strings.Contains(logStr, "module=powerdns step=handover outcome=ok") {
			t.Errorf("expected powerdns to have handover:\n%s", logStr)
		}
		if !strings.Contains(logStr, "module=vault step=handover outcome=ok") {
			t.Errorf("expected vault to have handover:\n%s", logStr)
		}
		if !strings.Contains(logStr, "secrets.migrate") {
			t.Errorf("expected secrets migration log:\n%s", logStr)
		}

		// --- Phase 2: Idempotent Second Apply ---
		t.Run("IdempotentSecondApply", func(t *testing.T) {
			var secondLogBuf bytes.Buffer
			eSecond := engine.New(stateDir, secretsStore)
			eSecond.Logger = slog.New(slog.NewTextHandler(&secondLogBuf, nil))
			eSecond.Allocator = alloc
			eSecond.Registry.SetNative("core.container/v1", broker.NativeContainer(rt))
			eSecond.Registry.SetNative("core.ansible/v1", broker.NativeAnsibleServer(ansibleServer))

			initialAnsibleCalls := ansibleServer.calls
			if err := eSecond.Run(ctx, resolved, plan); err != nil {
				t.Fatalf("engine.Run (Phase 2 second apply): %v\nLogs:\n%s", err, secondLogBuf.String())
			}

			secondLogStr := secondLogBuf.String()
			// Verifies no mutate actions were executed
			for _, action := range []string{"step=provision", "step=configure", "step=handover", "step=seed.up"} {
				if strings.Contains(secondLogStr, action) {
					t.Errorf("second apply executed forbidden action %q:\n%s", action, secondLogStr)
				}
			}

			// Ansible should not have received new configuration calls
			if ansibleServer.calls != initialAnsibleCalls {
				t.Errorf("expected 0 new ansible calls on second apply, got %d", ansibleServer.calls-initialAnsibleCalls)
			}

			stSecond, err := state.Load(stateDir)
			if err != nil {
				t.Fatalf("state.Load second apply: %v", err)
			}
			if !stSecond.SeedRetired {
				t.Errorf("SeedRetired should remain true")
			}
			if stSecond.SecretsBackend != "vault" {
				t.Errorf("SecretsBackend should remain 'vault'")
			}
		})
	})

	// --- Phase 3: Interrupted Apply and Clean Resumption ---
	t.Run("InterruptedApplyAndResume", func(t *testing.T) {
		cleanupGenesisContainers()
		t.Cleanup(cleanupGenesisContainers)

		stateDir := t.TempDir()
		secretsStore := newTestSecretsStore(t)

		vaultAnsible := &vaultFakeAnsibleServer{t: t}
		t.Cleanup(vaultAnsible.cleanup)

		cancelCtx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var interruptedOnce sync.Once
		ansibleServer := &e2eCompositeAnsibleServer{
			vaultFake: vaultAnsible,
			interrupter: func(req *ansiblev1.RunPlaybookRequest) error {
				playbook := string(req.GetPlaybookYaml())
				// Interrupt during chrony installation: chrony provision had finished, configure fails
				if strings.Contains(playbook, "install chrony") {
					var err error
					interruptedOnce.Do(func() {
						cancel()
						err = context.Canceled
					})
					return err
				}
				return nil
			},
		}

		var firstLogBuf bytes.Buffer
		eFirst := engine.New(stateDir, secretsStore)
		eFirst.Logger = slog.New(slog.NewTextHandler(&firstLogBuf, nil))
		eFirst.Allocator = alloc
		eFirst.Registry.SetNative("core.container/v1", broker.NativeContainer(rt))
		eFirst.Registry.SetNative("core.ansible/v1", broker.NativeAnsibleServer(ansibleServer))

		// First run should fail with interruption
		firstRunErr := eFirst.Run(cancelCtx, resolved, plan)
		if firstRunErr == nil {
			t.Fatal("expected first run to be interrupted and return error, got nil")
		}

		stInterrupted, err := state.Load(stateDir)
		if err != nil {
			t.Fatalf("state.Load after interruption: %v\nFirst run err: %v\nLogs:\n%s", err, firstRunErr, firstLogBuf.String())
		}
		if stInterrupted.SeedRetired {
			t.Error("SeedRetired should be false after interrupted apply")
		}

		// Clear interrupter so resumed run can complete
		ansibleServer.mu.Lock()
		ansibleServer.interrupter = nil
		ansibleServer.mu.Unlock()

		// Resumed run with fresh context on the same stateDir and secretsStore
		var resumeLogBuf bytes.Buffer
		eResumed := engine.New(stateDir, secretsStore)
		eResumed.Logger = slog.New(slog.NewTextHandler(&resumeLogBuf, nil))
		eResumed.Allocator = alloc
		eResumed.Registry.SetNative("core.container/v1", broker.NativeContainer(rt))
		eResumed.Registry.SetNative("core.ansible/v1", broker.NativeAnsibleServer(ansibleServer))

		if err := eResumed.Run(context.Background(), resolved, plan); err != nil {
			t.Fatalf("engine.Run resumed apply: %v\nLogs:\n%s", err, resumeLogBuf.String())
		}

		stResumed, err := state.Load(stateDir)
		if err != nil {
			t.Fatalf("state.Load after resumption: %v", err)
		}

		if !stResumed.SeedRetired {
			t.Errorf("expected SeedRetired to be true after resumed apply")
		}
		if stResumed.SecretsBackend != "vault" {
			t.Errorf("expected SecretsBackend to be 'vault' after resumed apply, got %q", stResumed.SecretsBackend)
		}

		for _, name := range expectedModules {
			if _, ok := stResumed.Modules[name]; !ok {
				t.Errorf("expected module %q in resumed state", name)
			}
		}
	})
}
