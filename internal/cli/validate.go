// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a spec: structure, module resolution, each module's Validate",
	}
	cmd.Flags().StringP("file", "f", "", "path of the YAML spec")
	_ = cmd.MarkFlagRequired("file")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		file, err := cmd.Flags().GetString("file")
		if err != nil {
			return err
		}
		resolved, p, err := resolveAndPlan(file)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if _, err := fmt.Fprintf(out, "%s: valid structure and module resolution.\n", file); err != nil {
			return err
		}

		var hasErrors bool
		for _, name := range p.Order {
			m := resolved.Modules[name]
			errs, err := validateModule(cmd.Context(), out, m)
			if err != nil {
				return err
			}
			if errs {
				hasErrors = true
			}
		}

		if hasErrors {
			return fmt.Errorf("module validation failed with errors")
		}
		_, err = fmt.Fprintln(out, "all modules validated successfully.")
		return err
	}
	return cmd
}

func validateModule(ctx context.Context, out io.Writer, m *resolver.Module) (hasErrors bool, err error) {
	client, err := modulehost.Launch(m.Installed.BinaryPath, m.Manifest)
	if err != nil {
		return false, fmt.Errorf("launching module %q: %w", m.Name, err)
	}
	defer client.Close()

	var cfg *structpb.Struct
	if m.Config != nil {
		s, err := structpb.NewStruct(m.Config)
		if err != nil {
			return false, fmt.Errorf("encoding config of %q: %w", m.Name, err)
		}
		cfg = s
	} else {
		cfg = &structpb.Struct{}
	}

	diags, err := client.Module().Validate(ctx, &modulev1.ValidateRequest{Config: cfg})
	if err != nil {
		return false, fmt.Errorf("validating module %q: %w", m.Name, err)
	}

	for _, d := range diags.GetDiagnostics() {
		severity := d.GetSeverity()
		if severity == "" {
			severity = "error"
		}
		prefix := ""
		if d.GetPath() != "" {
			prefix = d.GetPath() + ": "
		}
		if _, err := fmt.Fprintf(out, "[%s] %s: %s%s\n", severity, m.Name, prefix, d.GetMessage()); err != nil {
			return false, err
		}
		if severity != "warning" {
			hasErrors = true
		}
	}
	return hasErrors, nil
}
