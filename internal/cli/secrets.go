// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/secrets"
)

func newSecretsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage generated secrets (list, get)",
	}
	cmd.AddCommand(
		newSecretsListCmd(),
		newSecretsGetCmd(),
	)
	return cmd
}

// openSecretsStore loads the existing master key (fails if `genesis init` has
// not been run) and builds the `file` store.
func openSecretsStore(cmd *cobra.Command) (*secrets.FileStore, error) {
	stateDir, err := cmd.Flags().GetString("state-dir")
	if err != nil {
		return nil, err
	}
	provider := secrets.FileMasterKeyProvider{StateDir: stateDir}
	identity, err := provider.Load(cmd.Context())
	if err != nil {
		return nil, err
	}
	return secrets.NewFileStore(stateDir, identity), nil
}

func newSecretsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List generated secrets (metadata only, never a value)",
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		store, err := openSecretsStore(cmd)
		if err != nil {
			return err
		}
		entries, err := store.List(cmd.Context(), "")
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if len(entries) == 0 {
			_, err := fmt.Fprintln(out, "no secret.")
			return err
		}
		for _, e := range entries {
			if _, err := fmt.Fprintf(out, "%s\tkind=%s\towner=%s\trecovery=%v\tcreated_at=%s\n",
				e.Ref, e.Meta.Kind, e.Meta.Owner, e.Meta.Recovery, e.Meta.CreatedAt.Format("2006-01-02T15:04:05Z07:00")); err != nil {
				return err
			}
		}
		return nil
	}
	return cmd
}

func newSecretsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <reference>",
		Short: "Reveal the plaintext value of a generated secret",
		Args:  cobra.ExactArgs(1),
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		store, err := openSecretsStore(cmd)
		if err != nil {
			return err
		}
		value, err := store.Get(cmd.Context(), secrets.Ref(args[0]))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), value.ExposeSecret())
		return err
	}
	return cmd
}
