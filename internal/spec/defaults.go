// SPDX-License-Identifier: Apache-2.0

package spec

// Défauts déduits de l'exemple complet du doc 04 ; seule valeur de profile
// prise en charge à l'itération 1 (ADR-008).
const (
	defaultProfile          = "connected"
	defaultStateDir         = "/var/lib/genesis"
	defaultContainerRuntime = "auto"
)

func applyDefaults(env *Environment) {
	if env.Profile == "" {
		env.Profile = defaultProfile
	}
	if env.Seed.StateDir == "" {
		env.Seed.StateDir = defaultStateDir
	}
	if env.Seed.ContainerRuntime == "" {
		env.Seed.ContainerRuntime = defaultContainerRuntime
	}
}
