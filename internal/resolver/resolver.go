// SPDX-License-Identifier: Apache-2.0

// Package resolver associe les capacités demandées par la spec à des
// modules installés (docs/02-architecture.md).
package resolver

import (
	"fmt"
	"sort"
	"strings"

	sdk "genesis/sdk/go"
	"genesis/internal/modulehost"
	"genesis/internal/spec"
)

// CoreVersion est la version courante du cœur, comparée à la contrainte
// `core` de chaque manifest (docs/03-contrat-module.md §1).
const CoreVersion = "0.1.0"

// Module est un module retenu par la résolution.
type Module struct {
	Name      string
	Manifest  *sdk.ManifestFile
	Installed modulehost.Installed
	// AutoAdded est vrai si le module a été ajouté parce qu'il fournit une
	// fonction requise, sans être demandé explicitement dans la spec
	// (docs/04-spec.md : "le plan affiche les modules ajoutés").
	AutoAdded bool
}

// Resolved est le résultat de la résolution : l'ensemble des modules
// nécessaires, et la correspondance capacité → module.
type Resolved struct {
	Modules           map[string]*Module
	CapabilityModule  map[string]string
	FunctionProviders map[string]string // fonction (sans suffixe @phase) -> module
}

// Resolve associe chaque capacité de env à un module installé, ajoute
// automatiquement les modules requis manquants, et vérifie la compatibilité
// de version du cœur (docs/02-architecture.md).
func Resolve(env *spec.Environment, installed []modulehost.Installed) (*Resolved, error) {
	byName := make(map[string]modulehost.Installed, len(installed))
	for _, m := range installed {
		byName[m.Manifest.Name] = m
	}

	r := &Resolved{
		Modules:           map[string]*Module{},
		CapabilityModule:  map[string]string{},
		FunctionProviders: map[string]string{},
	}

	// 1. Capacité -> module (explicite ou défaut), doc04.
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
			return nil, fmt.Errorf("capacité %q : module %q non installé (genesis modules install)", capName, moduleName)
		}
		if err := addModule(r, installedModule, false); err != nil {
			return nil, err
		}
		r.CapabilityModule[capName] = moduleName
	}

	// 2. Fermeture des fonctions requises : ajoute automatiquement les
	// modules manquants jusqu'à point fixe (docs/02-architecture.md).
	if err := closeRequirements(r, byName); err != nil {
		return nil, err
	}

	// 3. Compatibilité de version du cœur (docs/03-contrat-module.md §1).
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
		return "", fmt.Errorf("capacité %q : aucun module installé ne la fournit", capability)
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
		return "", fmt.Errorf("capacité %q : plusieurs modules installés (%s), choix explicite requis (capabilities.%s.module)", capability, strings.Join(names, ", "), capability)
	}
}

func addModule(r *Resolved, installed modulehost.Installed, autoAdded bool) error {
	name := installed.Manifest.Name
	if existing, ok := r.Modules[name]; ok {
		if !autoAdded {
			existing.AutoAdded = false // une demande explicite l'emporte sur un ajout automatique
		}
		return registerProvides(r, existing)
	}
	m := &Module{Name: name, Manifest: installed.Manifest, Installed: installed, AutoAdded: autoAdded}
	r.Modules[name] = m
	return registerProvides(r, m)
}

func registerProvides(r *Resolved, m *Module) error {
	for _, p := range m.Manifest.Provides {
		if existing, ok := r.FunctionProviders[p.Function]; ok && existing != m.Name {
			return fmt.Errorf("fonction %q : fournie à la fois par %q et %q, choix explicite requis", p.Function, existing, m.Name)
		}
		r.FunctionProviders[p.Function] = m.Name
	}
	return nil
}

// closeRequirements ajoute, tant que nécessaire, les modules qui fournissent
// une fonction requise par un module déjà retenu mais non encore couverte.
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
					function, _ := splitFunctionPhase(entry.Function)
					if _, ok := r.FunctionProviders[function]; ok {
						continue
					}

					provider, err := findProvider(function, byName)
					if err != nil {
						if entry.Optional {
							continue
						}
						return fmt.Errorf("module %q requiert %q : %w", m.Name, entry.Function, err)
					}
					if provider == nil {
						if entry.Optional {
							continue
						}
						return fmt.Errorf("module %q requiert %q : aucun fournisseur installé", m.Name, entry.Function)
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

// orderedRequires trie les phases pour un parcours déterministe.
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

func findProvider(function string, byName map[string]modulehost.Installed) (*modulehost.Installed, error) {
	var candidates []modulehost.Installed
	for _, m := range byName {
		for _, p := range m.Manifest.Provides {
			if p.Function == function {
				candidates = append(candidates, m)
				break
			}
		}
	}
	switch len(candidates) {
	case 0:
		return nil, nil
	case 1:
		return &candidates[0], nil
	default:
		return nil, fmt.Errorf("plusieurs modules installés fournissent %q (%s), choix explicite requis", function, strings.Join(moduleNames(candidates), ", "))
	}
}

// splitFunctionPhase sépare le suffixe @seed/@target d'un nom de fonction
// requise (docs/03-contrat-module.md §1).
func splitFunctionPhase(function string) (name, phase string) {
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
