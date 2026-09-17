// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

func newSeedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Gère le cycle de vie de la graine",
	}
	cmd.AddCommand(newSeedRetireCmd())
	return cmd
}

func newSeedRetireCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retire",
		Short: "Arrête les services graine après passation",
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "J8")
	}
	return cmd
}
