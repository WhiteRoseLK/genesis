// SPDX-License-Identifier: Apache-2.0

// Package spec loads and validates the user spec (docs/04-spec.md).
//
// Module-specific validation (config_schema) is delegated to the module and
// arrives at milestone M3/M4; this package only does the overall structural
// validation described in milestone M1.
package spec

// Environment is the root of the user spec.
type Environment struct {
	APIVersion   string                `yaml:"apiVersion"`
	Kind         string                `yaml:"kind"`
	Metadata     Metadata              `yaml:"metadata"`
	Network      Network               `yaml:"network"`
	Seed         Seed                  `yaml:"seed"`
	Profile      string                `yaml:"profile"`
	Sizes        map[string]Size       `yaml:"sizes,omitempty"`
	Capabilities map[string]Capability `yaml:"capabilities"`
	Placement    map[string][]string   `yaml:"placement,omitempty"`
}

type Metadata struct {
	Name string `yaml:"name"`
}

type Network struct {
	CIDR        string   `yaml:"cidr"`
	Gateway     string   `yaml:"gateway"`
	Pool        string   `yaml:"pool"`
	Domain      string   `yaml:"domain"`
	UpstreamDNS []string `yaml:"upstream_dns,omitempty"`
	UpstreamNTP []string `yaml:"upstream_ntp,omitempty"`
}

type Seed struct {
	Address          string `yaml:"address"`
	StateDir         string `yaml:"state_dir,omitempty"`
	ContainerRuntime string `yaml:"container_runtime,omitempty"`
}

type Size struct {
	CPU      int `yaml:"cpu"`
	MemoryMB int `yaml:"memory_mb"`
	DiskGB   int `yaml:"disk_gb"`
}

// Capability is the user request for a capability (doc 04). Config stays
// opaque to the core: its module-specific validation is delegated to the
// module (milestone M3/M4).
type Capability struct {
	Module string         `yaml:"module,omitempty"`
	Config map[string]any `yaml:"config,omitempty"`
}
