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
		Short: "Validate a spec: structure, module resolution, each module's Validate",
	}
	cmd.Flags().StringP("file", "f", "", "path of the YAML spec")
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
		if _, err := fmt.Fprintf(out, "%s: valid structure.\n", file); err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, "module resolution and per-module validation: not implemented yet (see docs/08-milestones.md).")
		return err
	}
	return cmd
}
