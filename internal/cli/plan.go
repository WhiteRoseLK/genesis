// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"genesis/internal/modulehost"
	"genesis/internal/planner"
	"genesis/internal/resolver"
	"genesis/internal/spec"
)

func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Calcule le plan d'exécution pour une spec, avec couches et modules ajoutés automatiquement",
	}
	cmd.Flags().StringP("file", "f", "", "chemin de la spec YAML")
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

// resolveAndPlan charge la spec, découvre les modules installés, résout les
// capacités et construit le plan (docs/02-architecture.md).
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
	out := cmd.OutOrStdout()
	for i, name := range p.Order {
		marker := ""
		if resolved.Modules[name].AutoAdded {
			marker = " (ajouté automatiquement)"
		}
		if _, err := fmt.Fprintf(out, "%d. %s%s\n", i+1, name, marker); err != nil {
			return err
		}
	}
	return nil
}
