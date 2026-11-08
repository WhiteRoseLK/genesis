// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"fmt"
	"os"
	"path/filepath"
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

// assertCapabilityModules resolves the capabilities together against the real
// manifests and checks the module chosen for each. With explicit, the spec
// names the module; without, the default applies.
func assertCapabilityModules(t *testing.T, want map[string]string, explicit bool) {
	t.Helper()
	env := &spec.Environment{Capabilities: map[string]spec.Capability{"compute": {Module: "proxmox"}}}
	for capability, module := range want {
		c := spec.Capability{}
		if explicit {
			c.Module = module
		}
		env.Capabilities[capability] = c
	}
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
	assertCapabilityModules(t, defaultModules, false)
}

// The non-default providers stay selectable with an explicit module.
func TestRealManifestsExplicitAlternatives(t *testing.T) {
	assertCapabilityModules(t, map[string]string{"dns": "coredns", "pki": "step-ca"}, true)
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
