// SPDX-License-Identifier: Apache-2.0

// Package planner construit l'ordre de construction des modules résolus :
// un graphe dont les arêtes viennent des fonctions requises/fournies
// (docs/02-architecture.md), avec détection de cycle. Les nœuds sont des
// modules (comme l'illustre le diagramme du doc 05, time→dns→pki→bastion)
// même si le graphe est dérivé des dépendances entre fonctions.
package planner

import (
	"fmt"
	"sort"
	"strings"

	"genesis/internal/resolver"
)

// Edge documente pourquoi To dépend de From : la fonction concernée. Utile
// pour l'affichage du plan (docs/02 : "genesis plan").
type Edge struct {
	From     string
	To       string
	Function string
}

// Plan est l'ordre de construction des modules résolus.
type Plan struct {
	Order []string
	Edges []Edge
}

// Build construit l'ordre topologique des modules de resolved, détecte les
// cycles (critère d'acceptation du jalon J4, doc 08).
func Build(resolved *resolver.Resolved) (*Plan, error) {
	names := make([]string, 0, len(resolved.Modules))
	for name := range resolved.Modules {
		names = append(names, name)
	}
	sort.Strings(names)

	edges, err := computeEdges(resolved, names)
	if err != nil {
		return nil, err
	}

	order, err := topologicalSort(names, edges)
	if err != nil {
		return nil, err
	}

	return &Plan{Order: order, Edges: edges}, nil
}

func computeEdges(resolved *resolver.Resolved, names []string) ([]Edge, error) {
	var edges []Edge

	for _, name := range names {
		m := resolved.Modules[name]

		phases := make([]string, 0, len(m.Manifest.Requires))
		for phase := range m.Manifest.Requires {
			phases = append(phases, phase)
		}
		sort.Strings(phases)

		for _, phase := range phases {
			for _, entry := range m.Manifest.Requires[phase] {
				function, functionPhase := resolver.SplitFunctionPhase(entry.Function)
				provider, ok := resolved.ProviderFor(function, functionPhase)
				if !ok {
					if entry.Optional {
						continue
					}
					// Le résolveur aurait déjà dû échouer avant d'en arriver
					// là ; erreur défensive plutôt qu'un edge manquant silencieux.
					return nil, fmt.Errorf("module %q : fonction requise %q sans fournisseur résolu", name, entry.Function)
				}
				if provider == name {
					continue // un module qui se fournit lui-même n'est pas une dépendance d'ordre
				}
				edges = append(edges, Edge{From: provider, To: name, Function: entry.Function})
			}
		}
	}
	return edges, nil
}

// topologicalSort trie names par ordre de dépendance (Kahn), détecte les
// cycles.
func topologicalSort(names []string, edges []Edge) ([]string, error) {
	inDegree := make(map[string]int, len(names))
	adjacency := make(map[string][]string, len(names))
	for _, n := range names {
		inDegree[n] = 0
	}
	dedup := map[[2]string]bool{}
	for _, e := range edges {
		key := [2]string{e.From, e.To}
		if dedup[key] {
			continue
		}
		dedup[key] = true
		adjacency[e.From] = append(adjacency[e.From], e.To)
		inDegree[e.To]++
	}

	var ready []string
	for _, n := range names {
		if inDegree[n] == 0 {
			ready = append(ready, n)
		}
	}
	sort.Strings(ready)

	var order []string
	for len(ready) > 0 {
		sort.Strings(ready)
		n := ready[0]
		ready = ready[1:]
		order = append(order, n)

		next := append([]string(nil), adjacency[n]...)
		sort.Strings(next)
		for _, m := range next {
			inDegree[m]--
			if inDegree[m] == 0 {
				ready = append(ready, m)
			}
		}
	}

	if len(order) != len(names) {
		var stuck []string
		for _, n := range names {
			if inDegree[n] > 0 {
				stuck = append(stuck, n)
			}
		}
		sort.Strings(stuck)
		return nil, fmt.Errorf("cycle détecté entre les modules : %s", strings.Join(stuck, ", "))
	}

	return order, nil
}
