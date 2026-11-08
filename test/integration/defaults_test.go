// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/spec"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
)

// defaultModules is the table of ADR-135: the module a capability resolves to
// when the spec gives no `module:`.
var defaultModules = map[string]string{
	"compute": "proxmox",
	"time":    "chrony",
	"dns":     "powerdns",
	"pki":     "vault",
	"secrets": "vault",
	"bastion": "teleport",
}

// realManifests loads the manifest of every module of the repository.
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
		installed = append(installed, modulehost.Installed{Name: m.Name, Version: m.Version, Manifest: m})
	}
	return installed
}

// assertResolvesTo resolves the capabilities together against the real
// manifests and checks the module chosen for each.
func assertResolvesTo(t *testing.T, env *spec.Environment, want map[string]string) {
	t.Helper()
	r, err := resolver.Resolve(env, realManifests(t))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for capability, module := range want {
		if got := r.CapabilityModule[capability]; got != module {
			t.Errorf("CapabilityModule[%s] = %q, want %q", capability, got, module)
		}
	}
}

// Regression for #126: a capability without `module:` resolves to its default
// module with the real manifests.
func TestRealManifestsDefaultModules(t *testing.T) {
	env := &spec.Environment{Capabilities: map[string]spec.Capability{}}
	for capability := range defaultModules {
		env.Capabilities[capability] = spec.Capability{}
	}
	env.Capabilities["compute"] = spec.Capability{Module: "proxmox"}
	assertResolvesTo(t, env, defaultModules)
}

// The non-default providers stay selectable with an explicit module.
func TestRealManifestsExplicitAlternatives(t *testing.T) {
	want := map[string]string{"compute": "proxmox", "dns": "coredns", "pki": "step-ca"}
	env := &spec.Environment{Capabilities: map[string]spec.Capability{}}
	for capability, module := range want {
		env.Capabilities[capability] = spec.Capability{Module: module}
	}
	assertResolvesTo(t, env, want)
}

// The default modules documented in docs/04-spec.md match the table above.
func TestDefaultModulesAreDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../docs/04-spec.md")
	if err != nil {
		t.Fatal(err)
	}
	for capability, module := range defaultModules {
		if capability == "compute" {
			continue // chosen in the spec example, not in the defaults list
		}
		if want := fmt.Sprintf("`%s` → `%s`", capability, module); !strings.Contains(string(doc), want) {
			t.Errorf("docs/04-spec.md does not list %s", want)
		}
	}
}

// A module can only be the default of a capability it declares.
func TestManifestDefaultsAreDeclaredCapabilities(t *testing.T) {
	for _, m := range realManifests(t) {
		for capability, isDefault := range m.Manifest.Defaults {
			if !isDefault {
				continue
			}
			if !slices.Contains(m.Manifest.Capabilities, capability) {
				t.Errorf("module %s is default for %q, which is not in its capabilities %v", m.Name, capability, m.Manifest.Capabilities)
			}
		}
	}
}

// Regression for #136: the minimal spec (a compute provider and `bastion: {}`)
// resolves against the real manifests, the dependencies taking their defaults.
func TestRealManifestsMinimalSpecResolves(t *testing.T) {
	env := &spec.Environment{Capabilities: map[string]spec.Capability{
		"compute": {Module: "proxmox"},
		"bastion": {},
	}}
	r, err := resolver.Resolve(env, realManifests(t))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for function, module := range map[string]string{"dns.resolver/v1": "powerdns", "pki.issuer/v1": "vault", "time.ntp/v1": "chrony"} {
		if provider, _ := r.ProviderFor(function, ""); provider != module {
			t.Errorf("provider of %s = %q, want %q", function, provider, module)
		}
	}
}
