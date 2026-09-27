// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load lit, valide (structure générale + JSON Schema + refus des secrets
// littéraux) et décode la spec utilisateur, défauts appliqués.
//
// La résolution des capacités en modules et la validation propre à chaque
// module (config_schema) sont déléguées à des jalons ultérieurs (J3/J4,
// docs/08-milestones.md).
func Load(path string) (*Environment, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lecture de %s : %w", path, err)
	}

	var generic any
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("%s : YAML invalide : %w", path, err)
	}

	// jsonschema attend des types JSON natifs (float64 pour les nombres) ;
	// on repasse par un aller-retour JSON depuis le résultat du décodage YAML.
	jsonBytes, err := json.Marshal(generic)
	if err != nil {
		return nil, fmt.Errorf("%s : conversion JSON : %w", path, err)
	}
	var doc any
	if err := json.Unmarshal(jsonBytes, &doc); err != nil {
		return nil, fmt.Errorf("%s : conversion JSON : %w", path, err)
	}

	var errs []error
	errs = append(errs, validateSchema(doc)...)
	if len(errs) == 0 {
		// La validation des références n'a de sens que sur une structure déjà conforme.
		errs = append(errs, validateSecretRefs(doc, "")...)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s : spec invalide :\n%w", path, errors.Join(errs...))
	}

	var env Environment
	if err := yaml.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("%s : décodage : %w", path, err)
	}
	applyDefaults(&env)

	return &env, nil
}
