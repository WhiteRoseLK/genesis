// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/engine"
)

func newDestroyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Delete the target resources described by the spec",
	}
	cmd.Flags().StringP("file", "f", "", "path of the YAML spec")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().Bool("auto-approve", false, "do not ask for confirmation before destroying resources")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		file, err := cmd.Flags().GetString("file")
		if err != nil {
			return err
		}
		autoApprove, err := cmd.Flags().GetBool("auto-approve")
		if err != nil {
			return err
		}

		resolved, p, err := resolveAndPlan(file)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()

		if !autoApprove {
			if _, err := fmt.Fprintf(out, "Destroy all target resources in %s? [y/N] ", file); err != nil {
				return err
			}
			approved, err := confirm(cmd)
			if err != nil {
				return err
			}
			if !approved {
				_, err := fmt.Fprintln(out, "cancelled.")
				return err
			}
		}

		store, err := openSecretsStore(cmd)
		if err != nil {
			return err
		}
		stateDir, err := cmd.Flags().GetString("state-dir")
		if err != nil {
			return err
		}

		e := engine.New(stateDir, store)
		if err := e.Destroy(cmd.Context(), resolved, p); err != nil {
			return err
		}

		_, err = fmt.Fprintln(out, "destroy complete.")
		return err
	}
	return cmd
}
