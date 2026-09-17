// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Valide une spec : structure, résolution des modules, Validate de chaque module",
	}
	cmd.Flags().StringP("file", "f", "", "chemin de la spec YAML")
	_ = cmd.MarkFlagRequired("file")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "J1")
	}
	return cmd
}
