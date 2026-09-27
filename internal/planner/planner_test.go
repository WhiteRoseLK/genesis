// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/spec"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
)

func installedModule(name string, opts ...func(*sdk.ManifestFile)) modulehost.Installed {
	m := &sdk.ManifestFile{Name: name, Version: "0.1.0", Core: ">=0.1.0"}
	for _, opt := range opts {
		opt(m)
	}
	return modulehost.Installed{Name: name, Version: m.Version, Manifest: m}
}

func withCapabilities(caps ...string) func(*sdk.ManifestFile) {
	return func(m *sdk.ManifestFile) { m.Capabilities = caps }
}

func withProvides(functions ...string) func(*sdk.ManifestFile) {
	return func(m *sdk.ManifestFile) {
		for _, f := range functions {
			m.Provides = append(m.Provides, sdk.FunctionRef{Function: f, Phases: []string{"target"}})
		}
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

func resolveOrFatal(t *testing.T, env *spec.Environment, installed []modulehost.Installed) *resolver.Resolved {
	t.Helper()
	resolved, err := resolver.Resolve(env, installed)
	if err != nil {
		t.Fatalf("Resolve : %v", err)
	}
	return resolved
}

func indexOf(order []string, name string) int {
	for i, n := range order {
		if n == name {
			return i
		}
	}
	return -1
}

// TestBuildOrdersLikeDoc05Chain reproduit l'exemple du doc 05 :
// time -> dns -> pki+secrets -> bastion.
func TestBuildOrdersLikeDoc05Chain(t *testing.T) {
	env := &spec.Environment{
		Capabilities: map[string]spec.Capability{
			"time":    {Module: "chrony"},
			"dns":     {Module: "powerdns"},
			"pki":     {Module: "vault"},
			"bastion": {Module: "openssh-bastion"},
		},
	}
	installed := []modulehost.Installed{
		installedModule("chrony", withCapabilities("time"), withProvides("time.ntp/v1")),
		installedModule("powerdns",
			withCapabilities("dns"),
			withProvides("dns.zone/v1"),
			withRequires("target", sdk.RequireEntry{Function: "time.ntp/v1"}),
		),
		installedModule("vault",
			withCapabilities("pki"),
			withProvides("pki.issuer/v1"),
			withRequires("target", sdk.RequireEntry{Function: "dns.zone/v1"}),
		),
		installedModule("openssh-bastion",
			withCapabilities("bastion"),
			withRequires("target", sdk.RequireEntry{Function: "pki.issuer/v1"}),
		),
	}

	resolved := resolveOrFatal(t, env, installed)
	plan, err := Build(resolved)
	if err != nil {
		t.Fatalf("Build : %v", err)
	}

	if len(plan.Order) != 4 {
		t.Fatalf("Order = %v, attendu 4 modules", plan.Order)
	}
	if indexOf(plan.Order, "chrony") >= indexOf(plan.Order, "powerdns") {
		t.Errorf("chrony devrait précéder powerdns : %v", plan.Order)
	}
	if indexOf(plan.Order, "powerdns") >= indexOf(plan.Order, "vault") {
		t.Errorf("powerdns devrait précéder vault : %v", plan.Order)
	}
	if indexOf(plan.Order, "vault") >= indexOf(plan.Order, "openssh-bastion") {
		t.Errorf("vault devrait précéder openssh-bastion : %v", plan.Order)
	}
}

// TestBuildDetectsCycle est un critère d'acceptation du jalon J4 (doc 08).
func TestBuildDetectsCycle(t *testing.T) {
	env := &spec.Environment{
		Capabilities: map[string]spec.Capability{
			"a": {Module: "mod-a"},
			"b": {Module: "mod-b"},
		},
	}
	// mod-a requiert test.b/v1 (fourni par mod-b), mod-b requiert test.a/v1
	// (fourni par mod-a) : cycle direct entre les deux modules.
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
	resolved := resolveOrFatal(t, env, installed)

	_, err := Build(resolved)
	if err == nil {
		t.Fatal("cycle entre mod-a et mod-b : succès inattendu, devait être détecté")
	}
	t.Logf("cycle détecté comme attendu : %v", err)
}

// TestBuildInsertsNewDependentModuleAtCorrectPosition est le critère
// d'acceptation du jalon J4 (doc 08) : ajouter un module qui dépend d'un
// module existant l'insère au bon endroit dans le plan.
func TestBuildInsertsNewDependentModuleAtCorrectPosition(t *testing.T) {
	env := &spec.Environment{
		Capabilities: map[string]spec.Capability{
			"a": {Module: "test-a"},
			"c": {Module: "test-c"},
		},
	}
	base := []modulehost.Installed{
		installedModule("test-a", withCapabilities("a"), withProvides("test.a/v1")),
		installedModule("test-c",
			withCapabilities("c"),
			withProvides("test.c/v1"),
			withRequires("target", sdk.RequireEntry{Function: "test.a/v1"}),
		),
	}
	before := resolveOrFatal(t, env, base)
	beforePlan, err := Build(before)
	if err != nil {
		t.Fatalf("Build (avant test-d) : %v", err)
	}
	if len(beforePlan.Order) != 2 {
		t.Fatalf("Order avant test-d = %v, attendu 2 modules", beforePlan.Order)
	}

	// Ajout de test-d, qui dépend de test-a, sans toucher à rien d'autre.
	env.Capabilities["d"] = spec.Capability{Module: "test-d"}
	withD := append(append([]modulehost.Installed{}, base...),
		installedModule("test-d",
			withCapabilities("d"),
			withRequires("target", sdk.RequireEntry{Function: "test.a/v1"}),
		),
	)
	after := resolveOrFatal(t, env, withD)
	afterPlan, err := Build(after)
	if err != nil {
		t.Fatalf("Build (avec test-d) : %v", err)
	}

	if len(afterPlan.Order) != 3 {
		t.Fatalf("Order avec test-d = %v, attendu 3 modules", afterPlan.Order)
	}
	if indexOf(afterPlan.Order, "test-a") >= indexOf(afterPlan.Order, "test-d") {
		t.Errorf("test-a devrait précéder test-d : %v", afterPlan.Order)
	}
	// test-c et test-d dépendent tous deux de test-a mais pas l'un de
	// l'autre : leur ordre relatif n'est pas contraint, seul leur position
	// après test-a compte.
}

func TestBuildIsDeterministic(t *testing.T) {
	env := &spec.Environment{Capabilities: map[string]spec.Capability{
		"a": {Module: "test-a"}, "b": {Module: "test-b"}, "c": {Module: "test-c"},
	}}
	installed := []modulehost.Installed{
		installedModule("test-a", withCapabilities("a"), withProvides("test.a/v1")),
		installedModule("test-b", withCapabilities("b"), withProvides("test.b/v1")),
		installedModule("test-c", withCapabilities("c"), withProvides("test.c/v1")),
	}
	resolved := resolveOrFatal(t, env, installed)

	first, err := Build(resolved)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		again, err := Build(resolved)
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Order) != len(first.Order) {
			t.Fatalf("ordre non déterministe : %v vs %v", first.Order, again.Order)
		}
		for j := range first.Order {
			if first.Order[j] != again.Order[j] {
				t.Fatalf("ordre non déterministe : %v vs %v", first.Order, again.Order)
			}
		}
	}
}
