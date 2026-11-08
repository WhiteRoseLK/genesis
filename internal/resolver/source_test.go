// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/spec"
)

func TestResolveRecordsCapabilitySource(t *testing.T) {
	both := []modulehost.Installed{
		installedModule("powerdns", withCapabilities("dns"), withDefault("dns")),
		installedModule("bind", withCapabilities("dns")),
	}
	implicit := &spec.Environment{Capabilities: map[string]spec.Capability{"dns": {}}}
	tests := []struct {
		name      string
		env       *spec.Environment
		installed []modulehost.Installed
		want      CapabilitySource
	}{
		{"explicit module", specWithCapability("dns", "bind"), both, SourceExplicit},
		{"default module", implicit, both, SourceDefault},
		{"only provider without a defaults flag", implicit,
			[]modulehost.Installed{installedModule("powerdns", withCapabilities("dns"))}, SourceOnlyProvider},
		{"only provider that is also flagged default", implicit,
			[]modulehost.Installed{installedModule("powerdns", withCapabilities("dns"), withDefault("dns"))}, SourceOnlyProvider},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := Resolve(tt.env, tt.installed)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got := resolved.CapabilitySource["dns"]; got != tt.want {
				t.Errorf("CapabilitySource[dns] = %q, want %q", got, tt.want)
			}
		})
	}
}
