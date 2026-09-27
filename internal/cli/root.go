// SPDX-License-Identifier: Apache-2.0

// Package cli assembles the commands of the genesis CLI (see
// docs/02-architecture.md).
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/version"
)

// Execute builds the command tree and runs it.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// defaultStateDir is the default state directory (docs/04-spec.md,
// docs/06-secrets-state.md), used as long as no spec has been loaded (e.g.
// `genesis init`, before seed.state_dir is known).
const defaultStateDir = "/var/lib/genesis"

// NewRootCmd builds the root `genesis` command and all its children.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "genesis",
		Short:         "Build a self-sufficient environment foundation from a YAML spec",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.PersistentFlags().String("state-dir", defaultStateDir, "state directory of the seed (docs/06-secrets-state.md)")

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

// notImplemented returns an explicit error for a command whose implementation
// is planned for a later milestone (docs/08-milestones.md).
func notImplemented(cmd *cobra.Command, milestone string) error {
	return fmt.Errorf("command %q: not implemented yet (planned for %s, see docs/08-milestones.md)", cmd.CommandPath(), milestone)
}
