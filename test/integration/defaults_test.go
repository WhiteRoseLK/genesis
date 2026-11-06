// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"path/filepath"
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/spec"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
)

// realManifests loads the manifest of every real module except fake-compute
// (a test double that shares proxmox's compute default).
func realManifests(t *testing.T) []modulehost.Installed {
	t.Helper()
	paths, err := filepath.Glob("../../modules/*/module.yaml")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no module manifest found: %v", err)
	}
	var installed []modulehost.Installed
	for _, p := range paths {
		m, err := sdk.LoadManifest(p)
		if err != nil {
			t.Fatalf("LoadManifest(%s): %v", p, err)
		}
		if m.Name == "fake-compute" {
			continue
		}
		installed = append(installed, modulehost.Installed{Name: m.Name, Version: m.Version, Manifest: m})
	}
	return installed
}

func resolveCapabilities(t *testing.T, caps map[string]spec.Capability) *resolver.Resolved {
	t.Helper()
	r, err := resolver.Resolve(&spec.Environment{Capabilities: caps}, realManifests(t))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return r
}

// Regression for #126: a capability without `module:` resolves to its default
// module with the real manifests.
func TestRealManifestsDefaultModules(t *testing.T) {
	r := resolveCapabilities(t, map[string]spec.Capability{
		"compute": {Module: "proxmox"},
		"time":    {},
		"dns":     {},
		"pki":     {},
		"secrets": {},
		"bastion": {},
	})
	want := map[string]string{
		"time":    "chrony",
		"dns":     "powerdns",
		"pki":     "vault",
		"secrets": "vault",
		"bastion": "teleport",
	}
	for capability, module := range want {
		if got := r.CapabilityModule[capability]; got != module {
			t.Errorf("CapabilityModule[%s] = %q, want %q", capability, got, module)
		}
	}
}

// The non-default providers stay selectable with an explicit module.
func TestRealManifestsExplicitAlternatives(t *testing.T) {
	for capability, module := range map[string]string{"dns": "coredns", "pki": "step-ca"} {
		r := resolveCapabilities(t, map[string]spec.Capability{
			"compute":  {Module: "proxmox"},
			capability: {Module: module},
		})
		if got := r.CapabilityModule[capability]; got != module {
			t.Errorf("CapabilityModule[%s] = %q, want %q", capability, got, module)
		}
	}
}
