// SPDX-License-Identifier: Apache-2.0

// Package cli assemble les commandes du CLI genesis (voir docs/02-architecture.md).
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/version"
)

// Execute construit l'arbre de commandes et l'exécute.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// defaultStateDir est le répertoire d'état par défaut (docs/04-spec.md,
// docs/06-secrets-etat.md), utilisé tant qu'aucune spec n'a été chargée
// (ex. `genesis init`, avant que seed.state_dir ne soit connu).
const defaultStateDir = "/var/lib/genesis"

// NewRootCmd construit la commande racine `genesis` et tous ses enfants.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "genesis",
		Short:         "Construit un socle d'environnement autonome à partir d'une spec YAML",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.PersistentFlags().String("state-dir", defaultStateDir, "répertoire d'état de la graine (docs/06-secrets-etat.md)")

	root.AddCommand(
		newInitCmd(),
		newModulesCmd(),
		newValidateCmd(),
		newPlanCmd(),
		newApplyCmd(),
		newStatusCmd(),
		newSecretsCmd(),
		newDestroyCmd(),
	)

	return root
}

// notImplemented retourne une erreur explicite pour une commande dont
// l'implémentation est prévue à un jalon ultérieur (docs/08-jalons.md).
func notImplemented(cmd *cobra.Command, milestone string) error {
	return fmt.Errorf("commande %q : pas encore implémentée (prévue au %s, voir docs/08-jalons.md)", cmd.CommandPath(), milestone)
}
