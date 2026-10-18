// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/broker"
	"github.com/WhiteRoseLK/genesis/internal/engine"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/runner"
	"github.com/WhiteRoseLK/genesis/internal/state"
)

func requiresContainerOrAnsible(resolved *resolver.Resolved) bool {
	for _, m := range resolved.Modules {
		if m.Manifest == nil {
			continue
		}
		for _, reqs := range m.Manifest.Requires {
			for _, req := range reqs {
				if req.Function == "core.container/v1" || req.Function == "core.ansible/v1" {
					return true
				}
			}
		}
	}
	return false
}

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Run the plan: seed, target, handover, then seed retirement",
	}
	cmd.Flags().StringP("file", "f", "", "path of the YAML spec")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().Bool("auto-approve", false, "do not ask for confirmation before applying the plan")
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

		preferredRuntime := resolved.Seed.ContainerRuntime
		if rt, err := runner.DetectContainerRuntime(preferredRuntime); err == nil {
			e.Registry.SetNative("core.container/v1", broker.NativeContainer(rt, resolved.Seed.Address))
			e.Registry.SetNative("core.ansible/v1", broker.NativeAnsible(rt))
		} else if requiresContainerOrAnsible(resolved) {
			return fmt.Errorf("detecting container runtime: %w", err)
		}

		if err := e.Run(cmd.Context(), resolved, p); err != nil {
			return err
		}

		if _, err := fmt.Fprintln(out, "apply complete."); err != nil {
			return err
		}
		return printSeedStatus(out, stateDir)
	}
	return cmd
}

// printSeedStatus reports whether the seed has been retired and, if so, what
// to keep offline before deleting the seed machine
// (docs/05-bootstrap-lifecycle.md, phase 4).
func printSeedStatus(out io.Writer, stateDir string) error {
	st, err := state.Load(stateDir)
	if err != nil {
		return err
	}
	if !st.SeedRetired {
		_, err := fmt.Fprintln(out, "seed kept: not all of its functions have been taken over by the target yet (see the log).")
		return err
	}
	_, err = fmt.Fprintf(out, `seed retired. Keep offline before deleting the seed machine:
  - %s (master key, decrypts the recovery secrets)
  - %s (encrypted copy of the recovery secrets: CA root, unseal keys)
  - %s (state)
`, filepath.Join(stateDir, "master.key"), filepath.Join(stateDir, "secrets"), filepath.Join(stateDir, "state.json"))
	return err
}

func confirm(cmd *cobra.Command) (bool, error) {
	if _, err := fmt.Fprint(cmd.OutOrStdout(), "Apply this plan? [y/N] "); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
