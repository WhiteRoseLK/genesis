// SPDX-License-Identifier: Apache-2.0

package sdk

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// ManifestFile is the YAML form of module.yaml (docs/03-module-contract.md
// §1).
type ManifestFile struct {
	APIVersion   string                    `yaml:"apiVersion"`
	Name         string                    `yaml:"name"`
	Version      string                    `yaml:"version"`
	Description  string                    `yaml:"description"`
	Layer        string                    `yaml:"layer"`
	Core         string                    `yaml:"core"`
	Protocol     int32                     `yaml:"protocol"`
	Capabilities []string                  `yaml:"capabilities"`
	Provides     []FunctionRef             `yaml:"provides"`
	Requires     map[string][]RequireEntry `yaml:"requires"` // key: "seed" | "target"
	ConfigSchema string                    `yaml:"config_schema"`
	Secrets      []SecretDecl              `yaml:"secrets"`
	Resources    []ResourceDecl            `yaml:"resources"`
	Defaults     map[string]bool           `yaml:"defaults"`
}

// FunctionRef is a function provided by the module, with the phases in which
// it is provided (e.g. "pki.issuer/v1", phases: [seed, target]).
type FunctionRef struct {
	Function string   `yaml:"function"`
	Phases   []string `yaml:"phases"`
	// Fleet: a "fleet" function (docs/09-decisions.md ADR-017) — every
	// installed provider is called (fan-out), not a single chosen/repointable
	// active provider as for the other functions.
	Fleet bool `yaml:"fleet,omitempty"`
}

// RequireEntry is a function required by the module. The YAML accepts either a
// plain string ("compute.vm/v1") or an object {function, optional}
// (docs/10-adding-a-module.md, optional dependencies).
type RequireEntry struct {
	Function string `yaml:"function"`
	Optional bool   `yaml:"optional"`
}

// UnmarshalYAML accepts the short form (string) and the long form (object).
func (r *RequireEntry) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		r.Function = value.Value
		r.Optional = false
		return nil
	}
	type raw RequireEntry
	var tmp raw
	if err := value.Decode(&tmp); err != nil {
		return err
	}
	*r = RequireEntry(tmp)
	return nil
}

type SecretDecl struct {
	Name     string `yaml:"name"`
	Kind     string `yaml:"kind"`
	Rotation string `yaml:"rotation"`
	Recovery bool   `yaml:"recovery"`
}

type ResourceDecl struct {
	Role  string `yaml:"role"`
	Count int32  `yaml:"count"`
	Size  string `yaml:"size"`
}

// LoadManifest reads and decodes module.yaml.
func LoadManifest(path string) (*ManifestFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

// ParseManifest decodes the YAML content of an already read module.yaml
// (useful for a module that embeds its own manifest with go:embed, docs/10).
func ParseManifest(data []byte) (*ManifestFile, error) {
	var m ManifestFile
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	return &m, nil
}

// ToProto converts the YAML manifest into the protobuf Manifest message, as
// returned by Describe().
func (m *ManifestFile) ToProto() *modulev1.Manifest {
	provides := make([]*modulev1.FunctionRef, len(m.Provides))
	for i, p := range m.Provides {
		provides[i] = &modulev1.FunctionRef{Function: p.Function, Phases: p.Phases}
	}

	requires := make(map[string]*modulev1.RequireList, len(m.Requires))
	for phase, entries := range m.Requires {
		list := make([]*modulev1.RequireEntry, len(entries))
		for i, e := range entries {
			list[i] = &modulev1.RequireEntry{Function: e.Function, Optional: e.Optional}
		}
		requires[phase] = &modulev1.RequireList{Entries: list}
	}

	secrets := make([]*modulev1.SecretDecl, len(m.Secrets))
	for i, s := range m.Secrets {
		secrets[i] = &modulev1.SecretDecl{Name: s.Name, Kind: s.Kind, Rotation: s.Rotation, Recovery: s.Recovery}
	}

	resources := make([]*modulev1.ResourceDecl, len(m.Resources))
	for i, r := range m.Resources {
		resources[i] = &modulev1.ResourceDecl{Role: r.Role, Count: r.Count, Size: r.Size}
	}

	return &modulev1.Manifest{
		ApiVersion:   m.APIVersion,
		Name:         m.Name,
		Version:      m.Version,
		Description:  m.Description,
		Layer:        m.Layer,
		Core:         m.Core,
		Protocol:     m.Protocol,
		Capabilities: m.Capabilities,
		Provides:     provides,
		Requires:     requires,
		ConfigSchema: m.ConfigSchema,
		Secrets:      secrets,
		Resources:    resources,
		Defaults:     m.Defaults,
	}
}
