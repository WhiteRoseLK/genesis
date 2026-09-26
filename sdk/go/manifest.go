// SPDX-License-Identifier: Apache-2.0

package sdk

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// ManifestFile est la forme YAML de module.yaml (docs/03-contrat-module.md §1).
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
	Requires     map[string][]RequireEntry `yaml:"requires"` // clé : "seed" | "target"
	ConfigSchema string                    `yaml:"config_schema"`
	Secrets      []SecretDecl              `yaml:"secrets"`
	Resources    []ResourceDecl            `yaml:"resources"`
	Defaults     map[string]bool           `yaml:"defaults"`
}

// FunctionRef est une fonction fournie par le module, avec les phases où
// elle l'est (ex. "pki.issuer/v1", phases: [seed, target]).
type FunctionRef struct {
	Function string   `yaml:"function"`
	Phases   []string `yaml:"phases"`
	// Fleet : fonction « de parc » (docs/09-decisions.md ADR-017) — tous
	// les fournisseurs installés sont appelés (diffusion), pas un seul
	// fournisseur actif choisi/repointable comme le reste des fonctions.
	Fleet bool `yaml:"fleet,omitempty"`
}

// RequireEntry est une fonction requise par le module. Le YAML accepte soit
// une simple chaîne ("compute.vm/v1"), soit un objet {function, optional}
// (docs/10-ajouter-un-module.md, dépendances optionnelles).
type RequireEntry struct {
	Function string `yaml:"function"`
	Optional bool   `yaml:"optional"`
}

// UnmarshalYAML accepte la forme courte (chaîne) et la forme longue (objet).
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

// LoadManifest lit et décode module.yaml.
func LoadManifest(path string) (*ManifestFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lecture de %s : %w", path, err)
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return nil, fmt.Errorf("%s : %w", path, err)
	}
	return m, nil
}

// ParseManifest décode le contenu YAML d'un module.yaml déjà lu (utile à un
// module qui embarque son propre manifest via go:embed, docs/10).
func ParseManifest(data []byte) (*ManifestFile, error) {
	var m ManifestFile
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("YAML invalide : %w", err)
	}
	return &m, nil
}

// ToProto convertit le manifest YAML en message protobuf Manifest, tel que
// renvoyé par Describe().
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
