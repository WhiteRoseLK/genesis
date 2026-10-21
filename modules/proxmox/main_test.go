// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

func TestParseConfigTLS(t *testing.T) {
	t.Run("defaults without TLS fields", func(t *testing.T) {
		s, err := structpb.NewStruct(map[string]any{
			"endpoint": "https://pve.example.com:8006",
			"node":     "pve01",
		})
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := parseConfig(s)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.TLSInsecure {
			t.Errorf("TLSInsecure = %v, want false", cfg.TLSInsecure)
		}
		if cfg.TLSCACert != "" {
			t.Errorf("TLSCACert = %q, want empty", cfg.TLSCACert)
		}
	})

	t.Run("with TLS insecure and ca cert", func(t *testing.T) {
		s, err := structpb.NewStruct(map[string]any{
			"endpoint":     "https://pve.example.com:8006",
			"node":         "pve01",
			"tls_insecure": true,
			"tls_ca_cert":  "fake-ca-pem",
		})
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := parseConfig(s)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if !cfg.TLSInsecure {
			t.Errorf("TLSInsecure = %v, want true", cfg.TLSInsecure)
		}
		if cfg.TLSCACert != "fake-ca-pem" {
			t.Errorf("TLSCACert = %q, want fake-ca-pem", cfg.TLSCACert)
		}
	})
}

func TestEnsureClientTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"version":"8.1.3"}}`))
	}))
	t.Cleanup(srv.Close)

	caPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: srv.Certificate().Raw,
	})

	t.Run("with tls_insecure", func(t *testing.T) {
		m := &proxmoxModule{}
		s, err := structpb.NewStruct(map[string]any{
			"endpoint":     srv.URL,
			"node":         "pve01",
			"tls_insecure": true,
			"credentials": map[string]any{
				"token_id":     "genesis@pve!token",
				"token_secret": "secret",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		client, cfg, err := m.ensureClient(s)
		if err != nil {
			t.Fatalf("ensureClient: %v", err)
		}
		if client == nil || cfg == nil {
			t.Fatal("expected non-nil client and config")
		}
		v, err := client.Version(context.Background())
		if err != nil {
			t.Fatalf("Version failed: %v", err)
		}
		if v != "8.1.3" {
			t.Errorf("Version = %q, want 8.1.3", v)
		}
	})

	t.Run("with resolved tls_ca_cert PEM", func(t *testing.T) {
		m := &proxmoxModule{}
		s, err := structpb.NewStruct(map[string]any{
			"endpoint":    srv.URL,
			"node":        "pve01",
			"tls_ca_cert": string(caPEM),
			"credentials": map[string]any{
				"token_id":     "genesis@pve!token",
				"token_secret": "secret",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		client, cfg, err := m.ensureClient(s)
		if err != nil {
			t.Fatalf("ensureClient: %v", err)
		}
		if client == nil || cfg == nil {
			t.Fatal("expected non-nil client and config")
		}
		v, err := client.Version(context.Background())
		if err != nil {
			t.Fatalf("Version failed: %v", err)
		}
		if v != "8.1.3" {
			t.Errorf("Version = %q, want 8.1.3", v)
		}
	})

	t.Run("with tls_ca_cert_ref file:// reference", func(t *testing.T) {
		tmpDir := t.TempDir()
		caFile := filepath.Join(tmpDir, "ca.crt")
		if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
			t.Fatal(err)
		}

		m := &proxmoxModule{}
		s, err := structpb.NewStruct(map[string]any{
			"endpoint":        srv.URL,
			"node":            "pve01",
			"tls_ca_cert_ref": "file://" + caFile,
			"credentials": map[string]any{
				"token_id":     "genesis@pve!token",
				"token_secret": "secret",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		client, cfg, err := m.ensureClient(s)
		if err != nil {
			t.Fatalf("ensureClient: %v", err)
		}
		if client == nil || cfg == nil {
			t.Fatal("expected non-nil client and config")
		}
		v, err := client.Version(context.Background())
		if err != nil {
			t.Fatalf("Version failed: %v", err)
		}
		if v != "8.1.3" {
			t.Errorf("Version = %q, want 8.1.3", v)
		}
	})
}

func TestCheckTemplateLifecycle(t *testing.T) {
	var vms []map[string]any
	var deletedVMID int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api2/json/version":
			_, _ = w.Write([]byte(`{"data":{"version":"8.1.3"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api2/json/nodes/pve01/qemu":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": vms})
		case r.Method == http.MethodGet && r.URL.Path == "/api2/json/nodes/pve01/qemu/3000/status/current":
			_, _ = w.Write([]byte(`{"data":{"status":"stopped","template":1}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api2/json/nodes/pve01/qemu/3000/config":
			_, _ = w.Write([]byte(`{"data":{"description":"Genesis Base Template","name":"genesis-template"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api2/json/nodes/pve01/qemu/3000":
			deletedVMID = 3000
			_, _ = w.Write([]byte(`{"data":null}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfgStruct, err := structpb.NewStruct(map[string]any{
		"endpoint":     srv.URL,
		"node":         "pve01",
		"image":        "genesis-template",
		"tls_insecure": true,
		"credentials": map[string]any{
			"token_id":     "genesis@pve!token",
			"token_secret": "secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("check returns TODO when template missing", func(t *testing.T) {
		vms = nil
		m := &proxmoxModule{}
		res, err := m.Check(context.Background(), &modulev1.StepRequest{Config: cfgStruct})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if res.GetStatus() != modulev1.CheckResult_STATUS_TODO {
			t.Errorf("Check().Status = %v, want STATUS_TODO", res.GetStatus())
		}
	})

	t.Run("check returns COMPLIANT when template exists", func(t *testing.T) {
		vms = []map[string]any{{"vmid": 3000, "name": "genesis-template", "template": 1}}
		m := &proxmoxModule{}
		res, err := m.Check(context.Background(), &modulev1.StepRequest{Config: cfgStruct})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if res.GetStatus() != modulev1.CheckResult_STATUS_COMPLIANT {
			t.Errorf("Check().Status = %v, want STATUS_COMPLIANT", res.GetStatus())
		}
	})

	t.Run("destroy deletes managed template", func(t *testing.T) {
		vms = []map[string]any{{"vmid": 3000, "name": "genesis-template", "template": 1}}
		deletedVMID = 0
		m := &proxmoxModule{}
		st, _ := structpb.NewStruct(map[string]any{
			"template_name":    "genesis-template",
			"template_vmid":    3000,
			"template_managed": true,
		})
		_, err := m.Destroy(context.Background(), &modulev1.StepRequest{Config: cfgStruct, State: st})
		if err != nil {
			t.Fatalf("Destroy: %v", err)
		}
		if deletedVMID != 3000 {
			t.Errorf("deletedVMID = %d, want 3000", deletedVMID)
		}
	})

	t.Run("destroy preserves template if keep_template is true", func(t *testing.T) {
		vms = []map[string]any{{"vmid": 3000, "name": "genesis-template", "template": 1}}
		deletedVMID = 0
		keepCfg, _ := structpb.NewStruct(map[string]any{
			"endpoint":      srv.URL,
			"node":          "pve01",
			"image":         "genesis-template",
			"keep_template": true,
			"tls_insecure":  true,
			"credentials": map[string]any{
				"token_id":     "genesis@pve!token",
				"token_secret": "secret",
			},
		})
		st, _ := structpb.NewStruct(map[string]any{
			"template_name":    "genesis-template",
			"template_vmid":    3000,
			"template_managed": true,
		})
		m := &proxmoxModule{}
		_, err := m.Destroy(context.Background(), &modulev1.StepRequest{Config: keepCfg, State: st})
		if err != nil {
			t.Fatalf("Destroy: %v", err)
		}
		if deletedVMID != 0 {
			t.Errorf("deletedVMID = %d, want 0 (keep_template)", deletedVMID)
		}
	})
}
