// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

func newDestroyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Delete the target resources described by the spec",
	}
	cmd.Flags().StringP("file", "f", "", "path of the YAML spec")
	_ = cmd.MarkFlagRequired("file")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "iteration 2, issue #43")
	}
	return cmd
}
