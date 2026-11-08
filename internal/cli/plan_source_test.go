// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/resolver"
)

func TestDetermineModuleSourceShowsOrigin(t *testing.T) {
	tests := []struct {
		name   string
		source map[string]resolver.CapabilitySource
		auto   bool
		want   string
	}{
		{"explicit", map[string]resolver.CapabilitySource{"pki": resolver.SourceExplicit}, false, "declared in spec (pki)"},
		{"default", map[string]resolver.CapabilitySource{"pki": resolver.SourceDefault}, false, "declared in spec (pki, default)"},
		{"only provider", map[string]resolver.CapabilitySource{"pki": resolver.SourceOnlyProvider}, false, "declared in spec (pki, only provider)"},
		{"two capabilities, same origin", map[string]resolver.CapabilitySource{"pki": resolver.SourceDefault, "secrets": resolver.SourceDefault}, false, "declared in spec (pki, secrets, default)"},
		{"two capabilities, mixed origins", map[string]resolver.CapabilitySource{"pki": resolver.SourceExplicit, "secrets": resolver.SourceDefault}, false, "declared in spec (pki, secrets [default])"},
		{"automatic dependency", nil, true, "automatic (dependency)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := &resolver.Resolved{CapabilityModule: map[string]string{}, CapabilitySource: tt.source}
			for c := range tt.source {
				resolved.CapabilityModule[c] = "vault"
			}
			m := &resolver.Module{Name: "vault", AutoAdded: tt.auto}
			if got := determineModuleSource(m, resolved); got != tt.want {
				t.Errorf("determineModuleSource = %q, want %q", got, tt.want)
			}
		})
	}
}
