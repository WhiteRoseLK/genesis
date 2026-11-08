// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/broker"
	"github.com/WhiteRoseLK/genesis/internal/engine"
	"github.com/WhiteRoseLK/genesis/internal/runner"
	"github.com/WhiteRoseLK/genesis/internal/state"
)

func newCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Purge intermediate bootstrap artifacts (state, secrets, verifier VMs)",
	}
	cmd.Flags().StringP("file", "f", "", "path of the YAML spec (optional, to clean cluster verifier VMs)")
	cmd.Flags().Bool("auto-approve", false, "do not ask for confirmation before cleaning")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		autoApprove, err := cmd.Flags().GetBool("auto-approve")
		if err != nil {
			return err
		}

		stateDir, env, err := resolveStateDir(cmd)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()

		if !autoApprove {
			if _, err := fmt.Fprintf(out, "Clean intermediate bootstrap artifacts in %s? [y/N] ", stateDir); err != nil {
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

		// Failures are collected: the local cleanup always runs, but the
		// command must not claim success when a step failed (#152).
		errOut := cmd.ErrOrStderr()
		var failures []string
		fail := func(what string, err error, hint string) {
			failures = append(failures, what)
			_, _ = fmt.Fprintf(errOut, "Error: %s: %v\n  hint: %s\n", what, err, hint)
		}

		// 1. Stop any seed containers recorded in state
		if st, err := state.Load(stateDir); err == nil {
			preferredRuntime := "auto"
			if env != nil && env.Seed.ContainerRuntime != "" {
				preferredRuntime = env.Seed.ContainerRuntime
			}
			if rt, err := runner.DetectContainerRuntime(preferredRuntime); err == nil {
				for _, ms := range st.Modules {
					if len(ms.StateJSON) > 0 {
						var raw map[string]any
						if err := json.Unmarshal(ms.StateJSON, &raw); err == nil {
							if cid, ok := raw["container_id"].(string); ok && cid != "" {
								if err := rt.Stop(cmd.Context(), cid); err != nil {
									fail("could not stop seed container "+cid, err, "stop it manually with your container runtime")
								} else {
									_, _ = fmt.Fprintf(out, "stopped seed container %s\n", cid)
								}
							}
						}
					}
				}
			}
		}

		// 2. If spec is given, clean verifier VMs on the hypervisor
		if file != "" {
			const what = "could not clean the cluster verifier VMs"
			resolved, p, err := resolveAndPlan(file)
			if err != nil {
				fail(what, err, "install the modules the spec needs (genesis modules install) and re-run genesis clean -f "+file)
			} else if store, err := openSecretsStore(cmd, stateDir); err != nil {
				fail(what, err, "run genesis init to create the master key, then re-run genesis clean")
			} else {
				e := engine.New(stateDir, store)
				preferredRuntime := resolved.Seed.ContainerRuntime
				if rt, err := runner.DetectContainerRuntime(preferredRuntime); err == nil {
					e.Registry.SetNative("core.container/v1", broker.NativeContainer(rt, resolved.Seed.Address))
					e.Registry.SetNative("core.ansible/v1", broker.NativeAnsible(rt))
				}
				if err := e.CleanVerifierVMs(cmd.Context(), resolved, p); err != nil {
					fail(what, err, "check the compute module is installed and reachable, then re-run genesis clean")
				} else {
					_, _ = fmt.Fprintln(out, "cleaned cluster verifier VMs.")
				}
			}
		}

		// 3. Purge intermediate secrets directory (keep master.key)
		secretsDir := filepath.Join(stateDir, "secrets")
		if entries, err := os.ReadDir(secretsDir); err == nil {
			removed := true
			for _, entry := range entries {
				if err := os.RemoveAll(filepath.Join(secretsDir, entry.Name())); err != nil {
					removed = false
					fail("could not remove intermediate secret "+entry.Name(), err, "check the permissions of "+secretsDir)
				}
			}
			if removed {
				_, _ = fmt.Fprintf(out, "cleaned intermediate secrets in %s (master key preserved)\n", secretsDir)
			}
		}

		// 4. Remove state.lock if present
		_ = os.Remove(filepath.Join(stateDir, "state.lock"))

		// 5. Reset state.json to fresh empty state
		if err := state.Save(stateDir, state.New()); err != nil {
			fail("could not reset the state", err, "check the permissions of "+stateDir)
		} else {
			_, _ = fmt.Fprintf(out, "reset state in %s\n", filepath.Join(stateDir, "state.json"))
		}
		if len(failures) > 0 {
			return fmt.Errorf("clean incomplete: %s", strings.Join(failures, "; "))
		}
		_, _ = fmt.Fprintln(out, "clean complete.")
		return nil
	}
	return cmd
}
