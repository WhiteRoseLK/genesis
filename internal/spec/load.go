// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads, validates (overall structure + JSON Schema + rejection of
// literal secrets) and decodes the user spec, with defaults applied.
//
// Resolving capabilities into modules and module-specific validation
// (config_schema) are delegated to later milestones (M3/M4,
// docs/08-milestones.md).
func Load(path string) (*Environment, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lecture de %s: %w", path, err)
	}

	var generic any
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("%s: invalid YAML: %w", path, err)
	}

	// jsonschema expects native JSON types (float64 for numbers); we go
	// through a JSON round trip from the result of the YAML decode.
	jsonBytes, err := json.Marshal(generic)
	if err != nil {
		return nil, fmt.Errorf("%s: conversion JSON: %w", path, err)
	}
	var doc any
	if err := json.Unmarshal(jsonBytes, &doc); err != nil {
		return nil, fmt.Errorf("%s: conversion JSON: %w", path, err)
	}

	var errs []error
	errs = append(errs, validateSchema(doc)...)
	if len(errs) == 0 {
		// Validating references only makes sense on an already compliant
		// structure.
		errs = append(errs, validateSecretRefs(doc, "")...)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s: invalid spec:\n%w", path, errors.Join(errs...))
	}

	var env Environment
	if err := yaml.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("%s: decoding: %w", path, err)
	}
	applyDefaults(&env)

	return &env, nil
}
