// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"genesis/internal/engine"
)

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Exécute le plan jusqu'à la passation",
	}
	cmd.Flags().StringP("file", "f", "", "chemin de la spec YAML")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().Bool("auto-approve", false, "n'attend pas de confirmation avant d'appliquer le plan")
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
		if err := printPlan(cmd, resolved, p); err != nil {
			return err
		}

		if !autoApprove {
			approved, err := confirm(cmd)
			if err != nil {
				return err
			}
			if !approved {
				_, err := fmt.Fprintln(out, "annulé.")
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
		if err := e.Run(cmd.Context(), resolved, p); err != nil {
			return err
		}

		_, err = fmt.Fprintln(out, "apply terminé.")
		return err
	}
	return cmd
}

func confirm(cmd *cobra.Command) (bool, error) {
	if _, err := fmt.Fprint(cmd.OutOrStdout(), "Appliquer ce plan ? [y/N] "); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("lecture de la confirmation : %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes" || answer == "o" || answer == "oui", nil
}
