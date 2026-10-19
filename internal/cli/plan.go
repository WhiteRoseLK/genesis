// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/planner"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/spec"
)

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiGray   = "\033[90m"
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
	colorEnabled := isColorEnabled()

	fmt.Fprintf(&b, "Execution plan (%d modules):\n", len(p.Order))

	headers := []string{"#", "MODULE", "PHASE / LOCATION", "SOURCE"}
	rows := make([][]string, len(p.Order))

	for i, name := range p.Order {
		m := resolved.Modules[name]
		num := strconv.Itoa(i + 1)
		phase := determineModulePhase(m)
		source := determineModuleSource(m, resolved)
		rows[i] = []string{num, name, phase, source}
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

	// Print headers
	for i, h := range headers {
		if i > 0 {
			b.WriteString("  ")
		}
		padding := strings.Repeat(" ", widths[i]-len(h))
		b.WriteString(colorize(h, ansiBold, colorEnabled) + padding)
	}
	b.WriteString("\n")

	// Print separator line
	for i := range headers {
		if i > 0 {
			b.WriteString("  ")
		}
		sep := strings.Repeat("-", widths[i])
		b.WriteString(colorize(sep, ansiGray, colorEnabled))
	}
	b.WriteString("\n")

	// Print rows
	for _, row := range rows {
		for i, col := range row {
			if i > 0 {
				b.WriteString("  ")
			}
			padding := strings.Repeat(" ", widths[i]-len(col))
			color := getCellColor(i, col)
			b.WriteString(colorize(col, color, colorEnabled) + padding)
		}
		b.WriteString("\n")
	}

	// Print footer / legend
	b.WriteString("\n")
	b.WriteString(colorize("Phases:\n", ansiBold, colorEnabled))
	fmt.Fprintf(&b, "  • %s    : Temporary bootstrap services running in Docker on host (retired at end of apply).\n",
		colorize("Seed", ansiYellow, colorEnabled))
	fmt.Fprintf(&b, "  • %s  : Final production services running on dedicated VMs on the cluster.\n",
		colorize("Target", ansiGreen, colorEnabled))
	fmt.Fprintf(&b, "  • %s : Infrastructure drivers and management providers.\n",
		colorize("Control", ansiCyan, colorEnabled))

	out := cmd.OutOrStdout()
	_, err := fmt.Fprint(out, b.String())
	return err
}

func colorize(text, code string, enabled bool) string {
	if !enabled || code == "" {
		return text
	}
	return code + text + ansiReset
}

func isColorEnabled() bool {
	return os.Getenv("NO_COLOR") == ""
}

func getCellColor(colIdx int, val string) string {
	switch colIdx {
	case 0: // #
		return ansiGray
	case 1: // MODULE
		return ansiBold
	case 2: // PHASE / LOCATION
		if strings.HasPrefix(val, "Seed") {
			return ansiYellow
		}
		if strings.HasPrefix(val, "Target") {
			return ansiGreen
		}
		if strings.HasPrefix(val, "Control") {
			return ansiCyan
		}
		return ""
	case 3: // SOURCE
		if strings.HasPrefix(val, "automatic") {
			return ansiGray
		}
		return ""
	default:
		return ""
	}
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
