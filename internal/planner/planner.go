// SPDX-License-Identifier: Apache-2.0

// Package planner builds the build order of the resolved modules: a graph
// whose edges come from the required/provided functions
// (docs/02-architecture.md), with cycle detection. The nodes are modules (as
// the doc 05 diagram illustrates, time→dns→pki→bastion) even though the graph
// is derived from the dependencies between functions.
package planner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/WhiteRoseLK/genesis/internal/resolver"
)

// Edge records why To depends on From: the function concerned. Useful for
// displaying the plan (docs/02: "genesis plan").
type Edge struct {
	From     string
	To       string
	Function string
}

// Plan is the build order of the resolved modules.
type Plan struct {
	Order []string
	Edges []Edge
}

// Build computes the topological order of the modules in resolved and detects
// cycles (M4 acceptance criterion, doc 08).
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
					// The resolver should already have failed before getting
					// here; a defensive error rather than a silently missing
					// edge.
					return nil, fmt.Errorf("module %q: required function %q has no resolved provider", name, entry.Function)
				}
				if provider == name {
					continue // a module that provides for itself is not an ordering dependency
				}
				edges = append(edges, Edge{From: provider, To: name, Function: entry.Function})
			}
		}
	}
	return edges, nil
}

// topologicalSort sorts names in dependency order (Kahn) and detects cycles.
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
		return nil, fmt.Errorf("cycle detected between modules: %s", strings.Join(stuck, ", "))
	}

	return order, nil
}
