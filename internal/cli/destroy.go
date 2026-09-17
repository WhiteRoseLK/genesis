// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

func newDestroyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Supprime les ressources cibles décrites par la spec",
	}
	cmd.Flags().StringP("file", "f", "", "chemin de la spec YAML")
	_ = cmd.MarkFlagRequired("file")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "J4")
	}
	return cmd
}
