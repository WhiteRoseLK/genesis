// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/secrets"
	"github.com/WhiteRoseLK/genesis/internal/state"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Vérifie les prérequis graine et initialise l'état local et la clé maîtresse",
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		stateDir, err := cmd.Flags().GetString("state-dir")
		if err != nil {
			return err
		}

		release, err := state.Lock(stateDir)
		if err != nil {
			return err
		}
		defer func() { _ = release() }()

		out := cmd.OutOrStdout()

		provider := secrets.FileMasterKeyProvider{StateDir: stateDir}
		identity, created, err := provider.Ensure(cmd.Context())
		if err != nil {
			return err
		}
		if created {
			if _, err := fmt.Fprintln(out, "Clé maîtresse générée. Sauvegardez-la maintenant : elle ne sera plus jamais affichée."); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(out, identity.String()); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintf(out, "Déjà initialisé : clé maîtresse existante dans %s.\n", stateDir); err != nil {
			return err
		}

		if _, err := state.Load(stateDir); err != nil {
			if !errors.Is(err, state.ErrNotInitialized) {
				return err
			}
			if err := state.Save(stateDir, state.New()); err != nil {
				return err
			}
		}

		_, err = fmt.Fprintf(out, "state_dir prêt : %s\n", stateDir)
		return err
	}
	return cmd
}
