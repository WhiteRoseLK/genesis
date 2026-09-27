// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"strings"
	"testing"
)

func TestLoadValidFixtures(t *testing.T) {
	for _, name := range []string{"lab.yaml", "lab-minimal.yaml"} {
		t.Run(name, func(t *testing.T) {
			env, err := Load("testdata/valid/" + name)
			if err != nil {
				t.Fatalf("Load(%s) unexpected error: %v", name, err)
			}
			if env.Profile != "connected" {
				t.Errorf("profile = %q, want %q", env.Profile, "connected")
			}
			if env.Seed.StateDir != "/var/lib/genesis" {
				t.Errorf("seed.state_dir = %q, want %q", env.Seed.StateDir, "/var/lib/genesis")
			}
		})
	}
}

// TestLoadInvalidFixtures checks that each invalid spec fails with an error
// that refers to the YAML path concerned (M1 acceptance criterion, doc 08: "10
// invalid specs fail with a YAML path").
func TestLoadInvalidFixtures(t *testing.T) {
	cases := []struct {
		file       string
		wantInPath string // substring expected in the error (YAML path)
	}{
		{"01-missing-apiversion.yaml", "apiVersion"},
		{"02-bad-apiversion.yaml", "apiVersion"},
		{"03-bad-kind.yaml", "kind"},
		{"04-missing-metadata-name.yaml", "metadata.name"},
		{"05-missing-network-cidr.yaml", "network.cidr"},
		{"06-missing-network.yaml", "network"},
		{"07-missing-seed-address.yaml", "seed.address"},
		{"08-unsupported-profile.yaml", "profile"},
		{"09-empty-capabilities.yaml", "capabilities"},
		{"10-literal-secret.yaml", "capabilities.compute.config.credentials.token_id_ref"},
		{"11-incomplete-size.yaml", "sizes.small.disk_gb"},
		{"12-bad-placement-type.yaml", "placement.infra01"},
		{"13-unknown-toplevel-field.yaml", "bogus"},
	}

	if len(cases) < 10 {
		t.Fatalf("at least 10 invalid specs are needed, there are %d", len(cases))
	}

	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			env, err := Load("testdata/invalid/" + c.file)
			if err == nil {
				t.Fatalf("Load(%s): unexpected success (env=%+v)", c.file, env)
			}
			if !strings.Contains(err.Error(), c.wantInPath) {
				t.Errorf("Load(%s) error = %q, want it to contain %q", c.file, err.Error(), c.wantInPath)
			}
		})
	}
}
