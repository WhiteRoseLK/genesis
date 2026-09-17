// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Affiche l'état par module et par fonction (fournisseur actif)",
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "J4")
	}
	return cmd
}
