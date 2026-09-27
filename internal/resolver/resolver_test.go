// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"strings"
	"testing"
	"time"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/spec"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
)

func timeout() <-chan time.Time {
	return time.After(2 * time.Second)
}

func installedModule(name string, opts ...func(*sdk.ManifestFile)) modulehost.Installed {
	m := &sdk.ManifestFile{
		Name:    name,
		Version: "0.1.0",
		Core:    ">=0.1.0",
	}
	for _, opt := range opts {
		opt(m)
	}
	return modulehost.Installed{Name: name, Version: m.Version, Manifest: m}
}

func withCapabilities(caps ...string) func(*sdk.ManifestFile) {
	return func(m *sdk.ManifestFile) { m.Capabilities = caps }
}

func withDefault(cap string) func(*sdk.ManifestFile) {
	return func(m *sdk.ManifestFile) {
		if m.Defaults == nil {
			m.Defaults = map[string]bool{}
		}
		m.Defaults[cap] = true
	}
}

func withProvides(functions ...string) func(*sdk.ManifestFile) {
	return func(m *sdk.ManifestFile) {
		for _, f := range functions {
			m.Provides = append(m.Provides, sdk.FunctionRef{Function: f, Phases: []string{"target"}})
		}
	}
}

func withProvidesPhases(function string, phases ...string) func(*sdk.ManifestFile) {
	return func(m *sdk.ManifestFile) {
		m.Provides = append(m.Provides, sdk.FunctionRef{Function: function, Phases: phases})
	}
}

func withRequires(phase string, entries ...sdk.RequireEntry) func(*sdk.ManifestFile) {
	return func(m *sdk.ManifestFile) {
		if m.Requires == nil {
			m.Requires = map[string][]sdk.RequireEntry{}
		}
		m.Requires[phase] = append(m.Requires[phase], entries...)
	}
}

func withCore(constraint string) func(*sdk.ManifestFile) {
	return func(m *sdk.ManifestFile) { m.Core = constraint }
}

func specWithCapability(capName, moduleName string) *spec.Environment {
	return &spec.Environment{
		Capabilities: map[string]spec.Capability{
			capName: {Module: moduleName},
		},
	}
}

func TestResolveExplicitModule(t *testing.T) {
	env := specWithCapability("compute", "proxmox")
	installed := []modulehost.Installed{installedModule("proxmox", withCapabilities("compute"))}

	resolved, err := Resolve(env, installed)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.CapabilityModule["compute"] != "proxmox" {
		t.Errorf("CapabilityModule[compute] = %q, want proxmox", resolved.CapabilityModule["compute"])
	}
	if resolved.Modules["proxmox"].AutoAdded {
		t.Error("proxmox was requested explicitly and should not be AutoAdded")
	}
}

func TestResolveSingleInstalledIsImplicitDefault(t *testing.T) {
	env := &spec.Environment{Capabilities: map[string]spec.Capability{"compute": {}}}
	installed := []modulehost.Installed{installedModule("proxmox", withCapabilities("compute"))}

	resolved, err := Resolve(env, installed)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.CapabilityModule["compute"] != "proxmox" {
		t.Errorf("CapabilityModule[compute] = %q, want proxmox (the only installed module)", resolved.CapabilityModule["compute"])
	}
}

func TestResolveAmbiguousCapabilityWithoutDefaultFails(t *testing.T) {
	env := &spec.Environment{Capabilities: map[string]spec.Capability{"dns": {}}}
	installed := []modulehost.Installed{
		installedModule("powerdns", withCapabilities("dns")),
		installedModule("bind", withCapabilities("dns")),
	}
	if _, err := Resolve(env, installed); err == nil {
		t.Fatal("ambiguous capability without a default: unexpected success")
	}
}

func TestResolveAmbiguousCapabilityWithDefaultSucceeds(t *testing.T) {
	env := &spec.Environment{Capabilities: map[string]spec.Capability{"dns": {}}}
	installed := []modulehost.Installed{
		installedModule("powerdns", withCapabilities("dns"), withDefault("dns")),
		installedModule("bind", withCapabilities("dns")),
	}
	resolved, err := Resolve(env, installed)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.CapabilityModule["dns"] != "powerdns" {
		t.Errorf("CapabilityModule[dns] = %q, want powerdns (default module)", resolved.CapabilityModule["dns"])
	}
}

func TestResolveAutoAddsRequiredFunctionProvider(t *testing.T) {
	env := specWithCapability("bastion", "bastion-mod")
	installed := []modulehost.Installed{
		installedModule("bastion-mod",
			withCapabilities("bastion"),
			withRequires("target", sdk.RequireEntry{Function: "compute.vm/v1"}),
		),
		installedModule("proxmox", withCapabilities("compute"), withProvides("compute.vm/v1")),
	}

	resolved, err := Resolve(env, installed)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	proxmoxModule, ok := resolved.Modules["proxmox"]
	if !ok {
		t.Fatal("proxmox was not added automatically")
	}
	if !proxmoxModule.AutoAdded {
		t.Error("proxmox should have been marked AutoAdded")
	}
	if provider, _ := resolved.ProviderFor("compute.vm/v1", ""); provider != "proxmox" {
		t.Errorf("ProviderFor(compute.vm/v1) = %q, want proxmox", provider)
	}
}

func TestResolveMissingRequiredFunctionFails(t *testing.T) {
	env := specWithCapability("bastion", "bastion-mod")
	installed := []modulehost.Installed{
		installedModule("bastion-mod",
			withCapabilities("bastion"),
			withRequires("target", sdk.RequireEntry{Function: "compute.vm/v1"}),
		),
	}
	_, err := Resolve(env, installed)
	if err == nil {
		t.Fatal("required function without a provider: unexpected success")
	}
	if !strings.Contains(err.Error(), "compute.vm/v1") {
		t.Errorf("error = %q, want it to mention compute.vm/v1", err.Error())
	}
}

func TestResolveMissingOptionalFunctionSucceeds(t *testing.T) {
	env := specWithCapability("compute", "proxmox-install")
	installed := []modulehost.Installed{
		installedModule("proxmox-install",
			withCapabilities("compute"),
			withRequires("target", sdk.RequireEntry{Function: "platform.cluster/v1", Optional: true}),
		),
	}
	resolved, err := Resolve(env, installed)
	if err != nil {
		t.Fatalf("missing optional function: unexpected error: %v", err)
	}
	if _, ok := resolved.ProviderFor("platform.cluster/v1", ""); ok {
		t.Error("a missing optional function should not appear in FunctionProviders")
	}
}

func TestResolveIncompatibleCoreVersionFails(t *testing.T) {
	env := specWithCapability("compute", "proxmox")
	installed := []modulehost.Installed{
		installedModule("proxmox", withCapabilities("compute"), withCore(">=9.0.0 <10.0.0")),
	}
	if _, err := Resolve(env, installed); err == nil {
		t.Fatal("incompatible core constraint: unexpected success")
	}
}

// TestResolveMutualRequirementTerminates checks that the dependency closure
// does not loop forever when two already resolved modules require each other
// (the fixed point must be reached cleanly).
func TestResolveMutualRequirementTerminates(t *testing.T) {
	env := &spec.Environment{
		Capabilities: map[string]spec.Capability{
			"a": {Module: "mod-a"},
			"b": {Module: "mod-b"},
		},
	}
	installed := []modulehost.Installed{
		installedModule("mod-a",
			withCapabilities("a"),
			withProvides("test.a/v1"),
			withRequires("target", sdk.RequireEntry{Function: "test.b/v1"}),
		),
		installedModule("mod-b",
			withCapabilities("b"),
			withProvides("test.b/v1"),
			withRequires("target", sdk.RequireEntry{Function: "test.a/v1"}),
		),
	}

	done := make(chan struct{})
	var resolved *Resolved
	var err error
	go func() {
		resolved, err = Resolve(env, installed)
		close(done)
	}()
	select {
	case <-done:
	case <-timeout():
		t.Fatal("Resolve did not terminate (suspected infinite loop)")
	}
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved.Modules) != 2 {
		t.Errorf("resolved modules = %v, want 2", resolved.Modules)
	}
}

// TestResolveSeedAndTargetProvidersCoexist reflects the bootstrap pattern of
// doc 05: CoreDNS provides dns.zone/v1 in the seed phase, PowerDNS in the
// target phase — both at once, which is not a conflict to resolve.
func TestResolveSeedAndTargetProvidersCoexist(t *testing.T) {
	env := &spec.Environment{
		Capabilities: map[string]spec.Capability{
			"dns": {Module: "powerdns"},
		},
	}
	installed := []modulehost.Installed{
		installedModule("powerdns",
			withCapabilities("dns"),
			withProvidesPhases("dns.zone/v1", "target"),
			// Vault needs an initial TLS certificate signed by the seed before
			// the target even exists (docs/03, pki.issuer@seed example).
			withRequires("seed", sdk.RequireEntry{Function: "dns.zone/v1@seed"}),
		),
		installedModule("coredns", withProvidesPhases("dns.zone/v1", "seed")),
	}

	resolved, err := Resolve(env, installed)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if target, _ := resolved.ProviderFor("dns.zone/v1", "target"); target != "powerdns" {
		t.Errorf("ProviderFor(dns.zone/v1, target) = %q, want powerdns", target)
	}
	if seed, _ := resolved.ProviderFor("dns.zone/v1", "seed"); seed != "coredns" {
		t.Errorf("ProviderFor(dns.zone/v1, seed) = %q, want coredns", seed)
	}
	if _, ok := resolved.Modules["coredns"]; !ok {
		t.Error("coredns should have been added automatically for dns.zone/v1@seed")
	}
}

// TestResolveCarriesCapabilityConfigToModule checks that the spec's resolved
// config is passed to the module (StepRequest.config, doc 02/M5).
func TestResolveCarriesCapabilityConfigToModule(t *testing.T) {
	env := &spec.Environment{
		Capabilities: map[string]spec.Capability{
			"compute": {
				Module: "proxmox",
				Config: map[string]any{"endpoint": "https://pve01:8006", "node": "pve01"},
			},
		},
	}
	installed := []modulehost.Installed{installedModule("proxmox", withCapabilities("compute"))}

	resolved, err := Resolve(env, installed)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	cfg := resolved.Modules["proxmox"].Config
	if cfg["endpoint"] != "https://pve01:8006" || cfg["node"] != "pve01" {
		t.Errorf("Config = %+v, want the spec's endpoint/node", cfg)
	}
}
