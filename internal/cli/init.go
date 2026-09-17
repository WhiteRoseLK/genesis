// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Vérifie les prérequis graine et initialise l'état local et la clé maîtresse",
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "J2")
	}
	return cmd
}
