// SPDX-License-Identifier: Apache-2.0

package modulehost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverRejectsManifestNameMismatch(t *testing.T) {
	search := t.TempDir()
	writeFakeInstalledModule(t, search, "vault", "0.1.0")
	if err := os.Rename(filepath.Join(search, "vault"), filepath.Join(search, "renamed")); err != nil {
		t.Fatal(err)
	}

	_, err := Discover([]string{search})
	if err == nil {
		t.Fatal("Discover accepted a directory whose name differs from the manifest name")
	}
	for _, want := range []string{filepath.Join(search, "renamed", "0.1.0"), `"renamed"`, `"vault"`, "hint:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestDiscoverRejectsManifestVersionMismatch(t *testing.T) {
	search := t.TempDir()
	writeFakeInstalledModule(t, search, "vault", "0.1.0")
	if err := os.Rename(filepath.Join(search, "vault", "0.1.0"), filepath.Join(search, "vault", "9.9.9")); err != nil {
		t.Fatal(err)
	}

	_, err := Discover([]string{search})
	if err == nil {
		t.Fatal("Discover accepted a directory whose version differs from the manifest version")
	}
	for _, want := range []string{filepath.Join(search, "vault", "9.9.9"), `"9.9.9"`, `"0.1.0"`, "hint:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func writeFakeInstalledModule(t *testing.T, searchPath, name, version string) {
	t.Helper()
	dir := filepath.Join(searchPath, name, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: genesis/module/v1\n" +
		"name: " + name + "\n" +
		"version: " + version + "\n" +
		"description: \"module de test\"\n" +
		"layer: foundation\n" +
		"core: \">=0.1.0\"\n" +
		"protocol: 1\n" +
		"capabilities: []\n" +
		"provides: []\n" +
		"requires: {}\n" +
		"config_schema: schema.json\n" +
		"secrets: []\n" +
		"resources: []\n" +
		"defaults: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "module.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, BinaryName()), []byte("fake binary"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverFindsInstalledModules(t *testing.T) {
	search := t.TempDir()
	writeFakeInstalledModule(t, search, "vault", "0.1.0")
	writeFakeInstalledModule(t, search, "proxmox", "0.2.0")

	found, err := Discover([]string{search})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("Discover found %d module(s), want 2: %+v", len(found), found)
	}
	names := map[string]bool{}
	for _, m := range found {
		names[m.Name] = true
		if m.Manifest.Name != m.Name {
			t.Errorf("manifest.Name = %q, want %q", m.Manifest.Name, m.Name)
		}
	}
	if !names["vault"] || !names["proxmox"] {
		t.Errorf("modules found = %v, want vault and proxmox", names)
	}
}

func TestDiscoverIgnoresMissingSearchPath(t *testing.T) {
	found, err := Discover([]string{filepath.Join(t.TempDir(), "does-not-exist")})
	if err != nil {
		t.Fatalf("Discover on a missing path: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("Discover = %+v, want no module", found)
	}
}

func TestSearchPathsHonorsEnvVar(t *testing.T) {
	t.Setenv("GENESIS_MODULE_PATH", "/custom/path")
	paths := SearchPaths()
	if paths[0] != "/custom/path" {
		t.Errorf("SearchPaths()[0] = %q, want %q first", paths[0], "/custom/path")
	}
}
