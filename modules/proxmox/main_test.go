// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
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
