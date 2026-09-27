// SPDX-License-Identifier: Apache-2.0

// Package resolver maps the capabilities requested by the spec to installed
// modules (docs/02-architecture.md).
package resolver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/spec"
	coreversion "github.com/WhiteRoseLK/genesis/internal/version"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
)

// CoreVersion is the current version of the core, compared with the `core`
// constraint of each manifest (single source: internal/version).
const CoreVersion = coreversion.Version

// Module is a module selected by the resolution.
type Module struct {
	Name      string
	Manifest  *sdk.ManifestFile
	Installed modulehost.Installed
	// AutoAdded is true if the module was added because it provides a required
	// function, without being requested explicitly in the spec
	// (docs/04-spec.md: "the plan shows the added modules").
	AutoAdded bool
	// Config is the resolved config of the capabilities this module serves,
	// merged (doc 04: "two capabilities pointing to the same module share its
	// instance"). Passed as is in StepRequest.config by internal/engine.
	Config map[string]any
}

// Resolved is the result of the resolution: the set of modules needed, and the
// capability → module mapping.
type Resolved struct {
	Modules          map[string]*Module
	CapabilityModule map[string]string
	// FunctionProviders[function][phase] = the providing module for this
	// function in this phase ("seed" or "target") — the same function may have
	// different providers per phase (docs/05-bootstrap-lifecycle.md: CoreDNS
	// in the seed, PowerDNS as the target, both dns.zone/v1).
	FunctionProviders map[string]map[string]string
}

// ProviderFor returns the module providing function for phase. If phase is
// empty (required function without an @seed/@target suffix), the target
// provider is preferred and the seed is the fallback
// (docs/03-module-contract.md §1: "without a suffix, the active provider").
func (r *Resolved) ProviderFor(function, phase string) (string, bool) {
	byPhase, ok := r.FunctionProviders[function]
	if !ok {
		return "", false
	}
	if phase != "" {
		name, ok := byPhase[phase]
		return name, ok
	}
	if name, ok := byPhase["target"]; ok {
		return name, true
	}
	name, ok := byPhase["seed"]
	return name, ok
}

// Resolve maps each capability of env to an installed module, automatically
// adds the missing required modules, and checks the core version compatibility
// (docs/02-architecture.md).
func Resolve(env *spec.Environment, installed []modulehost.Installed) (*Resolved, error) {
	byName := make(map[string]modulehost.Installed, len(installed))
	for _, m := range installed {
		byName[m.Manifest.Name] = m
	}

	r := &Resolved{
		Modules:           map[string]*Module{},
		CapabilityModule:  map[string]string{},
		FunctionProviders: map[string]map[string]string{},
	}

	// 1. Capability -> module (explicit or default), doc 04.
	capNames := make([]string, 0, len(env.Capabilities))
	for name := range env.Capabilities {
		capNames = append(capNames, name)
	}
	sort.Strings(capNames)

	for _, capName := range capNames {
		capability := env.Capabilities[capName]
		moduleName := capability.Module
		if moduleName == "" {
			var err error
			moduleName, err = defaultModuleFor(capName, installed)
			if err != nil {
				return nil, err
			}
		}
		installedModule, ok := byName[moduleName]
		if !ok {
			return nil, fmt.Errorf("capability %q: module %q is not installed (genesis modules install)", capName, moduleName)
		}
		if err := addModule(r, installedModule, false); err != nil {
			return nil, err
		}
		r.CapabilityModule[capName] = moduleName

		m := r.Modules[moduleName]
		for k, v := range capability.Config {
			if m.Config == nil {
				m.Config = map[string]any{}
			}
			m.Config[k] = v
		}
	}

	// 2. Closure over required functions: automatically adds the missing
	//    modules up to a fixed point (docs/02-architecture.md).
	if err := closeRequirements(r, byName); err != nil {
		return nil, err
	}

	// 3. Core version compatibility (docs/03-module-contract.md §1).
	for _, m := range r.Modules {
		if err := checkCoreCompatibility(m.Name, m.Manifest.Core); err != nil {
			return nil, err
		}
	}

	return r, nil
}

func defaultModuleFor(capability string, installed []modulehost.Installed) (string, error) {
	var candidates []modulehost.Installed
	for _, m := range installed {
		if containsString(m.Manifest.Capabilities, capability) {
			candidates = append(candidates, m)
		}
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("capability %q: no installed module provides it", capability)
	case 1:
		return candidates[0].Manifest.Name, nil
	default:
		var defaults []modulehost.Installed
		for _, m := range candidates {
			if m.Manifest.Defaults[capability] {
				defaults = append(defaults, m)
			}
		}
		if len(defaults) == 1 {
			return defaults[0].Manifest.Name, nil
		}
		names := moduleNames(candidates)
		return "", fmt.Errorf("capability %q: several modules installed (%s), explicit choice required (capabilities.%s.module)", capability, strings.Join(names, ", "), capability)
	}
}

func addModule(r *Resolved, installed modulehost.Installed, autoAdded bool) error {
	name := installed.Manifest.Name
	if existing, ok := r.Modules[name]; ok {
		if !autoAdded {
			existing.AutoAdded = false // an explicit request takes precedence over an automatic addition
		}
		return registerProvides(r, existing)
	}
	m := &Module{Name: name, Manifest: installed.Manifest, Installed: installed, AutoAdded: autoAdded}
	r.Modules[name] = m
	return registerProvides(r, m)
}

func registerProvides(r *Resolved, m *Module) error {
	for _, p := range m.Manifest.Provides {
		phases := p.Phases
		if len(phases) == 0 {
			phases = []string{"target"}
		}
		byPhase, ok := r.FunctionProviders[p.Function]
		if !ok {
			byPhase = map[string]string{}
			r.FunctionProviders[p.Function] = byPhase
		}
		for _, phase := range phases {
			if existing, ok := byPhase[phase]; ok && existing != m.Name {
				return fmt.Errorf("function %q (phase %s): provided by both %q and %q, explicit choice required", p.Function, phase, existing, m.Name)
			}
			byPhase[phase] = m.Name
		}
	}
	return nil
}

// closeRequirements adds, as long as needed, the modules that provide a
// function required by an already selected module but not yet covered.
func closeRequirements(r *Resolved, byName map[string]modulehost.Installed) error {
	for {
		added := false

		names := make([]string, 0, len(r.Modules))
		for name := range r.Modules {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			m := r.Modules[name]
			for _, phaseEntries := range orderedRequires(m.Manifest.Requires) {
				for _, entry := range phaseEntries.entries {
					function, phase := SplitFunctionPhase(entry.Function)
					if _, ok := r.ProviderFor(function, phase); ok {
						continue
					}

					provider, err := findProvider(function, phase, byName)
					if err != nil {
						if entry.Optional {
							continue
						}
						return fmt.Errorf("module %q requires %q: %w", m.Name, entry.Function, err)
					}
					if provider == nil {
						if entry.Optional {
							continue
						}
						return fmt.Errorf("module %q requires %q: no provider installed", m.Name, entry.Function)
					}
					if err := addModule(r, *provider, true); err != nil {
						return err
					}
					added = true
				}
			}
		}

		if !added {
			return nil
		}
	}
}

type namedRequireList struct {
	phase   string
	entries []sdk.RequireEntry
}

// orderedRequires sorts the phases for a deterministic walk.
func orderedRequires(requires map[string][]sdk.RequireEntry) []namedRequireList {
	phases := make([]string, 0, len(requires))
	for phase := range requires {
		phases = append(phases, phase)
	}
	sort.Strings(phases)
	out := make([]namedRequireList, 0, len(phases))
	for _, phase := range phases {
		out = append(out, namedRequireList{phase: phase, entries: requires[phase]})
	}
	return out
}

// findProvider looks for an installed module (already resolved or not) that
// provides function for phase. An empty phase (unsuffixed require) prefers a
// target provider, with the seed as a fallback — both can coexist without
// being ambiguous with each other (docs/05-bootstrap-lifecycle.md).
func findProvider(function, phase string, byName map[string]modulehost.Installed) (*modulehost.Installed, error) {
	if phase != "" {
		return pickCandidate(function, providersForPhase(function, phase, byName))
	}
	if candidates := providersForPhase(function, "target", byName); len(candidates) > 0 {
		return pickCandidate(function, candidates)
	}
	return pickCandidate(function, providersForPhase(function, "seed", byName))
}

func providersForPhase(function, phase string, byName map[string]modulehost.Installed) []modulehost.Installed {
	var candidates []modulehost.Installed
	for _, m := range byName {
		for _, p := range m.Manifest.Provides {
			if p.Function != function {
				continue
			}
			phases := p.Phases
			if len(phases) == 0 {
				phases = []string{"target"}
			}
			if containsString(phases, phase) {
				candidates = append(candidates, m)
				break
			}
		}
	}
	return candidates
}

func pickCandidate(function string, candidates []modulehost.Installed) (*modulehost.Installed, error) {
	switch len(candidates) {
	case 0:
		return nil, nil
	case 1:
		return &candidates[0], nil
	default:
		return nil, fmt.Errorf("several installed modules provide %q (%s), explicit choice required", function, strings.Join(moduleNames(candidates), ", "))
	}
}

// SplitFunctionPhase splits the @seed/@target suffix off a required function
// name (docs/03-module-contract.md §1); reused by internal/planner.
func SplitFunctionPhase(function string) (name, phase string) {
	if i := strings.IndexByte(function, '@'); i >= 0 {
		return function[:i], function[i+1:]
	}
	return function, ""
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func moduleNames(installed []modulehost.Installed) []string {
	names := make([]string, len(installed))
	for i, m := range installed {
		names[i] = m.Manifest.Name
	}
	sort.Strings(names)
	return names
}
