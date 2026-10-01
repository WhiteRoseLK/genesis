// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/state"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the state per module and per function (active provider)",
	}
	cmd.Flags().StringP("file", "f", "", "path of the YAML spec (optional)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		stateDir, err := cmd.Flags().GetString("state-dir")
		if err != nil {
			return err
		}
		st, err := state.Load(stateDir)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if _, err := fmt.Fprintf(out, "Secrets backend: %s\n", st.SecretsBackend); err != nil {
			return err
		}
		if st.SeedRetired {
			if _, err := fmt.Fprintln(out, "Seed: retired"); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintln(out, "Seed: active"); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintln(out, "\nModules:"); err != nil {
			return err
		}
		if len(st.Modules) == 0 {
			if _, err := fmt.Fprintln(out, "  (none)"); err != nil {
				return err
			}
		} else {
			names := make([]string, 0, len(st.Modules))
			for name := range st.Modules {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				mState := st.Modules[name]
				var flags map[string]any
				if len(mState.StateJSON) > 0 {
					_ = json.Unmarshal(mState.StateJSON, &flags)
				}
				details := ""
				if len(flags) > 0 {
					var parts []string
					for k, v := range flags {
						parts = append(parts, fmt.Sprintf("%s=%v", k, v))
					}
					sort.Strings(parts)
					details = " (" + strings.Join(parts, ", ") + ")"
				}
				if _, err := fmt.Fprintf(out, "  - %s%s\n", name, details); err != nil {
					return err
				}
			}
		}

		file, _ := cmd.Flags().GetString("file")
		if file != "" {
			resolved, _, err := resolveAndPlan(file)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(out, "\nFunctions (active providers):"); err != nil {
				return err
			}
			var funcs []string
			for fn := range resolved.FunctionProviders {
				funcs = append(funcs, fn)
			}
			sort.Strings(funcs)
			for _, fn := range funcs {
				phase := "seed"
				if _, ok := resolved.ProviderFor(fn, "seed"); !ok || st.SeedRetired {
					phase = "target"
				}
				provider, ok := resolved.ProviderFor(fn, phase)
				if ok {
					if _, err := fmt.Fprintf(out, "  - %s: %s (%s)\n", fn, provider, phase); err != nil {
						return err
					}
				}
			}
		}

		return nil
	}
	return cmd
}
