// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/spec"
)

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Valide une spec : structure, résolution des modules, Validate de chaque module",
	}
	cmd.Flags().StringP("file", "f", "", "chemin de la spec YAML")
	_ = cmd.MarkFlagRequired("file")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		file, err := cmd.Flags().GetString("file")
		if err != nil {
			return err
		}
		if _, err := spec.Load(file); err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if _, err := fmt.Fprintf(out, "%s : structure valide.\n", file); err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, "résolution des modules et validation par module : pas encore implémentées (prévues aux J3/J4, voir docs/08-jalons.md).")
		return err
	}
	return cmd
}
