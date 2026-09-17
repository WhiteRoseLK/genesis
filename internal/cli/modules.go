// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

func newModulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "modules",
		Short: "Gère les modules installés (list, install, verify)",
	}
	cmd.AddCommand(
		newModulesListCmd(),
		newModulesInstallCmd(),
		newModulesVerifyCmd(),
	)
	return cmd
}

func newModulesListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Liste les modules installés",
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "J3")
	}
	return cmd
}

func newModulesInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <nom>@<version>",
		Short: "Installe un module",
		Args:  cobra.ExactArgs(1),
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "J3")
	}
	return cmd
}

func newModulesVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Vérifie l'empreinte des modules installés contre genesis.lock",
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "J3")
	}
	return cmd
}
