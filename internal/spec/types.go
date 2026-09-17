// SPDX-License-Identifier: Apache-2.0

// Package spec charge et valide la spec utilisateur (docs/04-spec.md).
//
// La validation propre à chaque module (config_schema) est déléguée au
// module et arrivera au jalon J3/J4 ; ce paquet ne fait que la validation
// structurelle générale décrite au jalon J1.
package spec

// Environment est la racine de la spec utilisateur.
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

// Capability est la demande utilisateur pour une capacité (doc 04). Config
// reste opaque au cœur : sa validation propre au module est déléguée au
// module (jalon J3/J4).
type Capability struct {
	Module string         `yaml:"module,omitempty"`
	Config map[string]any `yaml:"config,omitempty"`
}
