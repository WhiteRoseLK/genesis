// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Gère les secrets générés (list, get)",
	}
	cmd.AddCommand(
		newSecretsListCmd(),
		newSecretsGetCmd(),
	)
	return cmd
}

func newSecretsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Liste les secrets générés",
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "J2")
	}
	return cmd
}

func newSecretsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <nom>",
		Short: "Affiche un secret généré",
		Args:  cobra.ExactArgs(1),
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "J2")
	}
	return cmd
}
