// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Calcule le plan d'exécution pour une spec, avec couches et modules ajoutés automatiquement",
	}
	cmd.Flags().StringP("file", "f", "", "chemin de la spec YAML")
	_ = cmd.MarkFlagRequired("file")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "J4")
	}
	return cmd
}
