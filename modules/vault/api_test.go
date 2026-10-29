// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestVaultClientKVOperations(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		token := r.Header.Get("X-Vault-Token")
		if token != "test-token" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []string{"forbidden"}})
			return
		}

		path := r.URL.Path
		if r.Method == http.MethodGet && r.URL.Query().Get("list") == "true" {
			switch path {
			case "/v1/genesis/metadata", "/v1/genesis/metadata/":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"keys": []any{"app/", "standalone"},
					},
				})
			case "/v1/genesis/metadata/app", "/v1/genesis/metadata/app/":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"keys": []any{"secret1", "nested/"},
					},
				})
			case "/v1/genesis/metadata/app/nested", "/v1/genesis/metadata/app/nested/":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"keys": []any{"deep-secret"},
					},
				})
			case "/v1/genesis/metadata/empty", "/v1/genesis/metadata/empty/":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"keys": []any{},
					},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
			return
		}

		if r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/genesis/data/") {
			sub := strings.TrimPrefix(path, "/v1/genesis/data/")
			if sub == "standalone" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"data": map[string]any{
							"value": "secret-value",
						},
					},
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if r.Method == http.MethodPost && strings.HasPrefix(path, "/v1/genesis/data/") {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	cert := ts.Certificate()
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})

	client, err := newVaultClient(ts.URL, string(certPEM))
	if err != nil {
		t.Fatalf("newVaultClient: %v", err)
	}

	ctx := context.Background()

	t.Run("kvList root", func(t *testing.T) {
		keys, err := client.kvList(ctx, "test-token", "genesis", "")
		if err != nil {
			t.Fatalf("kvList: %v", err)
		}
		want := []string{"app/nested/deep-secret", "app/secret1", "standalone"}
		if !reflect.DeepEqual(keys, want) {
			t.Errorf("kvList('') = %v, want %v", keys, want)
		}
	})

	t.Run("kvList prefix app", func(t *testing.T) {
		keys, err := client.kvList(ctx, "test-token", "genesis", "app")
		if err != nil {
			t.Fatalf("kvList: %v", err)
		}
		want := []string{"app/nested/deep-secret", "app/secret1"}
		if !reflect.DeepEqual(keys, want) {
			t.Errorf("kvList('app') = %v, want %v", keys, want)
		}
	})

	t.Run("kvList prefix app/nested", func(t *testing.T) {
		keys, err := client.kvList(ctx, "test-token", "genesis", "app/nested")
		if err != nil {
			t.Fatalf("kvList: %v", err)
		}
		want := []string{"app/nested/deep-secret"}
		if !reflect.DeepEqual(keys, want) {
			t.Errorf("kvList('app/nested') = %v, want %v", keys, want)
		}
	})

	t.Run("kvList not found", func(t *testing.T) {
		keys, err := client.kvList(ctx, "test-token", "genesis", "nonexistent")
		if err != nil {
			t.Fatalf("kvList: %v", err)
		}
		if len(keys) != 0 {
			t.Errorf("kvList('nonexistent') = %v, want empty", keys)
		}
	})

	t.Run("kvList empty", func(t *testing.T) {
		keys, err := client.kvList(ctx, "test-token", "genesis", "empty")
		if err != nil {
			t.Fatalf("kvList: %v", err)
		}
		if len(keys) != 0 {
			t.Errorf("kvList('empty') = %v, want empty", keys)
		}
	})

	t.Run("kvRead existing", func(t *testing.T) {
		val, found, err := client.kvRead(ctx, "test-token", "genesis", "standalone")
		if err != nil {
			t.Fatalf("kvRead: %v", err)
		}
		if !found || val != "secret-value" {
			t.Errorf("kvRead = (%q, %v), want ('secret-value', true)", val, found)
		}
	})

	t.Run("kvRead not found", func(t *testing.T) {
		val, found, err := client.kvRead(ctx, "test-token", "genesis", "nonexistent")
		if err != nil {
			t.Fatalf("kvRead: %v", err)
		}
		if found || val != "" {
			t.Errorf("kvRead = (%q, %v), want ('', false)", val, found)
		}
	})

	t.Run("kvWrite", func(t *testing.T) {
		if err := client.kvWrite(ctx, "test-token", "genesis", "standalone", "updated"); err != nil {
			t.Fatalf("kvWrite: %v", err)
		}
	})
}

func TestVaultClientPKIOperations(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		token := r.Header.Get("X-Vault-Token")
		if token != "test-token" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"errors": []string{"forbidden"}})
			return
		}

		path := r.URL.Path
		switch path {
		case "/v1/pki/root/generate/exported":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if body["common_name"] != "Test Root CA" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"certificate": "-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----",
					"private_key": "-----BEGIN PRIVATE KEY-----\nKEY\n-----END PRIVATE KEY-----",
				},
			})
			return

		case "/v1/pki/root/sign-intermediate":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if body["csr"] != "test-csr" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if mpl, ok := body["max_path_length"].(float64); !ok || mpl != 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"certificate": "-----BEGIN CERTIFICATE-----\nINTERMEDIATE\n-----END CERTIFICATE-----",
					"issuing_ca":  "-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----",
				},
			})
			return

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	cert := ts.Certificate()
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})

	client, err := newVaultClient(ts.URL, string(certPEM))
	if err != nil {
		t.Fatalf("newVaultClient: %v", err)
	}

	ctx := context.Background()

	t.Run("generateExportedRoot", func(t *testing.T) {
		rootCert, rootKey, err := client.generateExportedRoot(ctx, "test-token", "pki", "Test Root CA", "87600h")
		if err != nil {
			t.Fatalf("generateExportedRoot: %v", err)
		}
		if !strings.Contains(rootCert, "ROOT") || !strings.Contains(rootKey, "KEY") {
			t.Errorf("unexpected cert/key returned: cert=%q, key=%q", rootCert, rootKey)
		}
	})

	t.Run("signIntermediate", func(t *testing.T) {
		intCert, issuingCA, err := client.signIntermediate(ctx, "test-token", "pki", "test-csr", "Test Intermediate CA", "87600h")
		if err != nil {
			t.Fatalf("signIntermediate: %v", err)
		}
		if !strings.Contains(intCert, "INTERMEDIATE") || !strings.Contains(issuingCA, "ROOT") {
			t.Errorf("unexpected cert/issuingCA returned: cert=%q, issuingCA=%q", intCert, issuingCA)
		}
	})
}

func TestVaultCAChain(t *testing.T) {
	m := &vaultModule{}

	ctx := context.Background()
	_, err := m.CAChain(ctx, nil)
	if err == nil {
		t.Fatal("expected error when vault is not configured")
	}

	m.pkiIntPEM = "-----BEGIN CERTIFICATE-----\nINT\n-----END CERTIFICATE-----"
	m.rootCAPEM = "-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----"

	resp, err := m.CAChain(ctx, nil)
	if err != nil {
		t.Fatalf("CAChain: %v", err)
	}
	wantChain := "-----BEGIN CERTIFICATE-----\nINT\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nROOT\n-----END CERTIFICATE-----\n"
	if resp.GetChainPem() != wantChain {
		t.Errorf("CAChain = %q, want %q", resp.GetChainPem(), wantChain)
	}
}
