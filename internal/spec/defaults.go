// SPDX-License-Identifier: Apache-2.0

package spec

// Defaults taken from the full example of doc 04; the only profile value
// supported in iteration 1 (ADR-008).
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
