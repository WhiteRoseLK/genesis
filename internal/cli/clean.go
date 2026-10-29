// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/broker"
	"github.com/WhiteRoseLK/genesis/internal/engine"
	"github.com/WhiteRoseLK/genesis/internal/runner"
	"github.com/WhiteRoseLK/genesis/internal/spec"
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

		stateDir, err := cmd.Flags().GetString("state-dir")
		if err != nil {
			return err
		}
		if file != "" && !cmd.Flags().Changed("state-dir") {
			if env, err := spec.Load(file); err == nil && env.Seed.StateDir != "" {
				stateDir = env.Seed.StateDir
			}
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

		// 1. Stop any seed containers recorded in state
		if st, err := state.Load(stateDir); err == nil {
			preferredRuntime := "auto"
			if file != "" {
				if env, err := spec.Load(file); err == nil && env.Seed.ContainerRuntime != "" {
					preferredRuntime = env.Seed.ContainerRuntime
				}
			}
			if rt, err := runner.DetectContainerRuntime(preferredRuntime); err == nil {
				for _, ms := range st.Modules {
					if len(ms.StateJSON) > 0 {
						var raw map[string]any
						if err := json.Unmarshal(ms.StateJSON, &raw); err == nil {
							if cid, ok := raw["container_id"].(string); ok && cid != "" {
								_ = rt.Stop(cmd.Context(), cid)
								_, _ = fmt.Fprintf(out, "stopped seed container %s\n", cid)
							}
						}
					}
				}
			}
		}

		// 2. If spec is given, clean verifier VMs on the hypervisor
		if file != "" {
			if resolved, p, err := resolveAndPlan(file); err == nil {
				store, err := openSecretsStore(cmd, stateDir)
				if err == nil {
					e := engine.New(stateDir, store)
					preferredRuntime := resolved.Seed.ContainerRuntime
					if rt, err := runner.DetectContainerRuntime(preferredRuntime); err == nil {
						e.Registry.SetNative("core.container/v1", broker.NativeContainer(rt, resolved.Seed.Address))
						e.Registry.SetNative("core.ansible/v1", broker.NativeAnsible(rt))
					}
					e.CleanVerifierVMs(cmd.Context(), resolved, p)
					_, _ = fmt.Fprintln(out, "cleaned cluster verifier VMs.")
				}
			}
		}

		// 3. Purge intermediate secrets directory (keep master.key)
		secretsDir := filepath.Join(stateDir, "secrets")
		if entries, err := os.ReadDir(secretsDir); err == nil {
			for _, entry := range entries {
				_ = os.RemoveAll(filepath.Join(secretsDir, entry.Name()))
			}
			_, _ = fmt.Fprintf(out, "cleaned intermediate secrets in %s (master key preserved)\n", secretsDir)
		}

		// 4. Remove state.lock if present
		_ = os.Remove(filepath.Join(stateDir, "state.lock"))

		// 5. Reset state.json to fresh empty state
		_ = state.Save(stateDir, state.New())
		_, _ = fmt.Fprintf(out, "reset state in %s\n", filepath.Join(stateDir, "state.json"))
		_, _ = fmt.Fprintln(out, "clean complete.")
		return nil
	}
	return cmd
}
