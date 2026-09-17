// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Exécute le plan jusqu'à la passation",
	}
	cmd.Flags().StringP("file", "f", "", "chemin de la spec YAML")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().Bool("auto-approve", false, "n'attend pas de confirmation avant d'appliquer le plan")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "J4")
	}
	return cmd
}
