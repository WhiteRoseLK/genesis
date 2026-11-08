// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/spec"
)

// resolveStateDir returns the state directory of the command: --state-dir when
// explicit, else the seed.state_dir of the spec given with -f, else the flag
// default. An unusable spec is an error, never a silent fallback to the
// default directory. When the directory comes from the spec it is made
// absolute and announced on stderr. The loaded spec, if any, is returned so
// callers need not load it again.
func resolveStateDir(cmd *cobra.Command) (string, *spec.Environment, error) {
	stateDir, err := cmd.Flags().GetString("state-dir")
	if err != nil {
		return "", nil, err
	}
	file, _ := cmd.Flags().GetString("file")
	if file == "" || cmd.Flags().Changed("state-dir") {
		return stateDir, nil, nil
	}
	env, err := spec.Load(file)
	if err != nil {
		return "", nil, err
	}
	if env.Seed.StateDir != "" {
		stateDir = env.Seed.StateDir
	}
	abs, err := filepath.Abs(stateDir)
	if err != nil {
		return "", nil, fmt.Errorf("resolving the state directory %q: %w", stateDir, err)
	}
	if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "state directory: %s (from %s)\n", abs, file); err != nil {
		return "", nil, err
	}
	return abs, env, nil
}
