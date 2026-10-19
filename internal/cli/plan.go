// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/planner"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/spec"
)

func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Compute the execution plan for a spec, with layers and automatically added modules",
	}
	cmd.Flags().StringP("file", "f", "", "path of the YAML spec")
	_ = cmd.MarkFlagRequired("file")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		file, err := cmd.Flags().GetString("file")
		if err != nil {
			return err
		}
		resolved, p, err := resolveAndPlan(file)
		if err != nil {
			return err
		}
		return printPlan(cmd, resolved, p)
	}
	return cmd
}

// resolveAndPlan loads the spec, discovers the installed modules, resolves the
// capabilities and builds the plan (docs/02-architecture.md).
func resolveAndPlan(file string) (*resolver.Resolved, *planner.Plan, error) {
	env, err := spec.Load(file)
	if err != nil {
		return nil, nil, err
	}
	found, err := modulehost.Discover(modulehost.SearchPaths())
	if err != nil {
		return nil, nil, err
	}
	resolved, err := resolver.Resolve(env, found)
	if err != nil {
		return nil, nil, err
	}
	p, err := planner.Build(resolved)
	if err != nil {
		return nil, nil, err
	}
	return resolved, p, nil
}

func printPlan(cmd *cobra.Command, resolved *resolver.Resolved, p *planner.Plan) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Execution plan (%d modules):\n", len(p.Order))

	headers := []string{"#", "MODULE", "PHASE / LOCATION", "FUNCTIONS PROVIDED", "SOURCE"}
	rows := make([][]string, len(p.Order))

	for i, name := range p.Order {
		m := resolved.Modules[name]
		num := strconv.Itoa(i + 1)
		phase := determineModulePhase(m)
		funcs := determineModuleFunctions(m)
		source := determineModuleSource(m, resolved)
		rows[i] = []string{num, name, phase, funcs, source}
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, col := range row {
			if len(col) > widths[i] {
				widths[i] = len(col)
			}
		}
	}

	for i, h := range headers {
		if i > 0 {
			b.WriteString("  ")
		}
		fmt.Fprintf(&b, "%-*s", widths[i], h)
	}
	b.WriteString("\n")

	for i := range headers {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(strings.Repeat("-", widths[i]))
	}
	b.WriteString("\n")

	for _, row := range rows {
		for i, col := range row {
			if i > 0 {
				b.WriteString("  ")
			}
			fmt.Fprintf(&b, "%-*s", widths[i], col)
		}
		b.WriteString("\n")
	}

	b.WriteString("\nPhases:\n")
	b.WriteString("  • Seed    : Temporary bootstrap services running in Docker on host (retired at end of apply).\n")
	b.WriteString("  • Target  : Final production services running on dedicated VMs on the cluster.\n")
	b.WriteString("  • Control : Infrastructure drivers and management providers.\n")

	out := cmd.OutOrStdout()
	_, err := fmt.Fprint(out, b.String())
	return err
}

func determineModulePhase(m *resolver.Module) string {
	if m == nil || m.Manifest == nil {
		return "Unknown"
	}
	hasSeed := false
	hasTarget := false
	requiresVM := false
	providesCompute := false

	for _, p := range m.Manifest.Provides {
		if p.Function == "compute.vm/v1" {
			providesCompute = true
		}
		for _, ph := range p.Phases {
			if ph == "seed" {
				hasSeed = true
			}
			if ph == "target" {
				hasTarget = true
			}
		}
	}
	for _, reqs := range m.Manifest.Requires {
		for _, req := range reqs {
			if req.Function == "compute.vm/v1" {
				requiresVM = true
			}
		}
	}

	if providesCompute {
		return "Control (hypervisor)"
	}
	if hasSeed && !hasTarget {
		return "Seed (temporary container)"
	}
	if requiresVM {
		return "Target (dedicated VM)"
	}
	if hasTarget {
		for _, p := range m.Manifest.Provides {
			if p.Function == "os.base/v1" {
				return "Target (VM configuration)"
			}
		}
		return "Target (cluster service)"
	}
	return "Control (host / OS utility)"
}

func determineModuleFunctions(m *resolver.Module) string {
	if m == nil || m.Manifest == nil {
		return "-"
	}
	var fns []string
	seen := map[string]bool{}
	for _, p := range m.Manifest.Provides {
		fn := p.Function
		if !seen[fn] {
			seen[fn] = true
			fns = append(fns, fn)
		}
	}
	if len(fns) == 0 {
		return "-"
	}
	return strings.Join(fns, ", ")
}

func determineModuleSource(m *resolver.Module, resolved *resolver.Resolved) string {
	if m.AutoAdded {
		return "automatic (dependency)"
	}
	var caps []string
	for capName, modName := range resolved.CapabilityModule {
		if modName == m.Name {
			caps = append(caps, capName)
		}
	}
	if len(caps) > 0 {
		sort.Strings(caps)
		return fmt.Sprintf("declared in spec (%s)", strings.Join(caps, ", "))
	}
	return "declared in spec"
}
