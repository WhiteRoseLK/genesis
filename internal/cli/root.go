// SPDX-License-Identifier: Apache-2.0

// Package cli assemble les commandes du CLI genesis (voir docs/02-architecture.md).
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Execute construit l'arbre de commandes et l'exécute.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// NewRootCmd construit la commande racine `genesis` et tous ses enfants.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "genesis",
		Short:         "Construit un socle d'environnement autonome à partir d'une spec YAML",
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.AddCommand(
		newInitCmd(),
		newModulesCmd(),
		newValidateCmd(),
		newPlanCmd(),
		newApplyCmd(),
		newStatusCmd(),
		newSecretsCmd(),
		newSeedCmd(),
		newDestroyCmd(),
	)

	return root
}

// notImplemented retourne une erreur explicite pour une commande dont
// l'implémentation est prévue à un jalon ultérieur (docs/08-jalons.md).
func notImplemented(cmd *cobra.Command, milestone string) error {
	return fmt.Errorf("commande %q : pas encore implémentée (prévue au %s, voir docs/08-jalons.md)", cmd.CommandPath(), milestone)
}
