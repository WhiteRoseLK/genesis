// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/planner"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
)

const legendLine = "Legend: #name = default module, @name = only provider"

func TestPlanShowsCapabilityOrigin(t *testing.T) {
	const (
		x = resolver.SourceExplicit
		d = resolver.SourceDefault
		o = resolver.SourceOnlyProvider
	)
	tests := []struct {
		name       string
		source     map[string]resolver.CapabilitySource
		auto       bool
		want       string
		wantLegend bool
	}{
		{"explicit", map[string]resolver.CapabilitySource{"pki": x}, false, "declared in spec (pki)", false},
		{"default", map[string]resolver.CapabilitySource{"pki": d}, false, "declared in spec (#pki)", true},
		{"only provider", map[string]resolver.CapabilitySource{"pki": o}, false, "declared in spec (@pki)", true},
		{"two defaults", map[string]resolver.CapabilitySource{"pki": d, "secrets": d}, false, "declared in spec (#pki, #secrets)", true},
		{"explicit and default", map[string]resolver.CapabilitySource{"pki": x, "secrets": d}, false, "declared in spec (pki, #secrets)", true},
		{"default and only provider", map[string]resolver.CapabilitySource{"pki": d, "secrets": o}, false, "declared in spec (#pki, @secrets)", true},
		{"automatic dependency", nil, true, "automatic (dependency)", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &resolver.Resolved{
				Modules:          map[string]*resolver.Module{},
				CapabilityModule: map[string]string{},
				CapabilitySource: tt.source,
			}
			for c := range tt.source {
				resolved.CapabilityModule[c] = "vault"
			}
			m := &resolver.Module{Name: "vault", AutoAdded: tt.auto}
			resolved.Modules["vault"] = m

			if got := determineModuleSource(m, resolved); got != tt.want {
				t.Errorf("determineModuleSource = %q, want %q", got, tt.want)
			}

			cmd := &cobra.Command{}
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			if err := printPlan(cmd, resolved, &planner.Plan{Order: []string{"vault"}}); err != nil {
				t.Fatalf("printPlan: %v", err)
			}
			if got := strings.Contains(buf.String(), legendLine); got != tt.wantLegend {
				t.Errorf("legend present = %v, want %v\n%s", got, tt.wantLegend, buf.String())
			}
		})
	}
}
