// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/spf13/cobra"

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the state per module and per function (active provider)",
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return notImplemented(cmd, "iteration 2, issue #43")
	}
	return cmd
}
