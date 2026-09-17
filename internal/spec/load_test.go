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
				t.Fatalf("Load(%s) inattendu : %v", name, err)
			}
			if env.Profile != "connected" {
				t.Errorf("profile = %q, attendu %q", env.Profile, "connected")
			}
			if env.Seed.StateDir != "/var/lib/genesis" {
				t.Errorf("seed.state_dir = %q, attendu %q", env.Seed.StateDir, "/var/lib/genesis")
			}
		})
	}
}

// TestLoadInvalidFixtures vérifie que chaque spec invalide échoue avec une
// erreur qui référence le chemin YAML concerné (critère d'acceptation J1,
// doc 08 : "10 specs invalides échouent avec chemin YAML").
func TestLoadInvalidFixtures(t *testing.T) {
	cases := []struct {
		file       string
		wantInPath string // sous-chaîne attendue dans l'erreur (chemin YAML)
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
		t.Fatalf("il faut au moins 10 specs invalides, il y en a %d", len(cases))
	}

	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			env, err := Load("testdata/invalid/" + c.file)
			if err == nil {
				t.Fatalf("Load(%s) : succès inattendu (env=%+v)", c.file, env)
			}
			if !strings.Contains(err.Error(), c.wantInPath) {
				t.Errorf("Load(%s) erreur = %q, attendu qu'elle contienne %q", c.file, err.Error(), c.wantInPath)
			}
		})
	}
}
