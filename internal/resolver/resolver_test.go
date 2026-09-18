// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"strings"
	"testing"
	"time"

	sdk "genesis/sdk/go"
	"genesis/internal/modulehost"
	"genesis/internal/spec"
)

func timeout() <-chan time.Time {
	return time.After(2 * time.Second)
}

func installedModule(name string, opts ...func(*sdk.ManifestFile)) modulehost.Installed {
	m := &sdk.ManifestFile{
		Name:    name,
		Version: "0.1.0",
		Core:    ">=0.1.0 <0.2.0",
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
		t.Fatalf("Resolve : %v", err)
	}
	if resolved.CapabilityModule["compute"] != "proxmox" {
		t.Errorf("CapabilityModule[compute] = %q, attendu proxmox", resolved.CapabilityModule["compute"])
	}
	if resolved.Modules["proxmox"].AutoAdded {
		t.Error("proxmox demandé explicitement, ne devrait pas être AutoAdded")
	}
}

func TestResolveSingleInstalledIsImplicitDefault(t *testing.T) {
	env := &spec.Environment{Capabilities: map[string]spec.Capability{"compute": {}}}
	installed := []modulehost.Installed{installedModule("proxmox", withCapabilities("compute"))}

	resolved, err := Resolve(env, installed)
	if err != nil {
		t.Fatalf("Resolve : %v", err)
	}
	if resolved.CapabilityModule["compute"] != "proxmox" {
		t.Errorf("CapabilityModule[compute] = %q, attendu proxmox (seul module installé)", resolved.CapabilityModule["compute"])
	}
}

func TestResolveAmbiguousCapabilityWithoutDefaultFails(t *testing.T) {
	env := &spec.Environment{Capabilities: map[string]spec.Capability{"dns": {}}}
	installed := []modulehost.Installed{
		installedModule("powerdns", withCapabilities("dns")),
		installedModule("bind", withCapabilities("dns")),
	}
	if _, err := Resolve(env, installed); err == nil {
		t.Fatal("capacité ambiguë sans défaut : succès inattendu")
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
		t.Fatalf("Resolve : %v", err)
	}
	if resolved.CapabilityModule["dns"] != "powerdns" {
		t.Errorf("CapabilityModule[dns] = %q, attendu powerdns (module par défaut)", resolved.CapabilityModule["dns"])
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
		t.Fatalf("Resolve : %v", err)
	}
	proxmoxModule, ok := resolved.Modules["proxmox"]
	if !ok {
		t.Fatal("proxmox n'a pas été ajouté automatiquement")
	}
	if !proxmoxModule.AutoAdded {
		t.Error("proxmox aurait dû être marqué AutoAdded")
	}
	if provider, _ := resolved.ProviderFor("compute.vm/v1", ""); provider != "proxmox" {
		t.Errorf("ProviderFor(compute.vm/v1) = %q, attendu proxmox", provider)
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
		t.Fatal("fonction requise sans fournisseur : succès inattendu")
	}
	if !strings.Contains(err.Error(), "compute.vm/v1") {
		t.Errorf("erreur = %q, attendu qu'elle mentionne compute.vm/v1", err.Error())
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
		t.Fatalf("fonction optionnelle absente : erreur inattendue : %v", err)
	}
	if _, ok := resolved.ProviderFor("platform.cluster/v1", ""); ok {
		t.Error("une fonction optionnelle absente ne devrait pas apparaître dans FunctionProviders")
	}
}

func TestResolveIncompatibleCoreVersionFails(t *testing.T) {
	env := specWithCapability("compute", "proxmox")
	installed := []modulehost.Installed{
		installedModule("proxmox", withCapabilities("compute"), withCore(">=9.0.0 <10.0.0")),
	}
	if _, err := Resolve(env, installed); err == nil {
		t.Fatal("contrainte core incompatible : succès inattendu")
	}
}

// TestResolveMutualRequirementTerminates vérifie que la fermeture des
// dépendances ne boucle pas indéfiniment quand deux modules déjà résolus se
// requièrent mutuellement (le point fixe doit être atteint proprement).
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
		t.Fatal("Resolve n'a pas terminé (boucle infinie suspectée)")
	}
	if err != nil {
		t.Fatalf("Resolve : %v", err)
	}
	if len(resolved.Modules) != 2 {
		t.Errorf("modules résolus = %v, attendu 2", resolved.Modules)
	}
}

// TestResolveSeedAndTargetProvidersCoexist reflète le schéma de bootstrap du
// doc 05 : CoreDNS fournit dns.zone/v1 en phase graine, PowerDNS en phase
// cible — les deux à la fois, ce n'est pas un conflit à résoudre.
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
			// Vault a besoin d'un certificat TLS initial signé par la graine
			// avant même que la cible existe (docs/03, exemple pki.issuer@seed).
			withRequires("seed", sdk.RequireEntry{Function: "dns.zone/v1@seed"}),
		),
		installedModule("coredns", withProvidesPhases("dns.zone/v1", "seed")),
	}

	resolved, err := Resolve(env, installed)
	if err != nil {
		t.Fatalf("Resolve : %v", err)
	}
	if target, _ := resolved.ProviderFor("dns.zone/v1", "target"); target != "powerdns" {
		t.Errorf("ProviderFor(dns.zone/v1, target) = %q, attendu powerdns", target)
	}
	if seed, _ := resolved.ProviderFor("dns.zone/v1", "seed"); seed != "coredns" {
		t.Errorf("ProviderFor(dns.zone/v1, seed) = %q, attendu coredns", seed)
	}
	if _, ok := resolved.Modules["coredns"]; !ok {
		t.Error("coredns aurait dû être ajouté automatiquement pour dns.zone/v1@seed")
	}
}
