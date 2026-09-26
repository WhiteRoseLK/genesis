// SPDX-License-Identifier: Apache-2.0

package spec

import (
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

var (
	missingPropertiesPattern = regexp.MustCompile(`^missing properties: (.+)$`)
	quotedNamePattern        = regexp.MustCompile(`'([^']+)'`)
)

//go:embed schema.json
var schemaJSON string

var envSchema = mustCompileSchema()

func mustCompileSchema() *jsonschema.Schema {
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("environment.json", strings.NewReader(schemaJSON)); err != nil {
		panic(fmt.Sprintf("spec: schéma JSON invalide : %v", err))
	}
	schema, err := compiler.Compile("environment.json")
	if err != nil {
		panic(fmt.Sprintf("spec: compilation du schéma JSON : %v", err))
	}
	return schema
}

// validateSchema valide doc (types JSON natifs) contre le schéma de
// l'enveloppe générale et retourne une erreur par échec, avec son chemin.
func validateSchema(doc any) []error {
	err := envSchema.Validate(doc)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []error{&ValidationError{Message: err.Error()}}
	}
	return flattenValidationError(ve)
}

func flattenValidationError(ve *jsonschema.ValidationError) []error {
	if len(ve.Causes) == 0 {
		return leafErrors(ve)
	}
	var errs []error
	for _, cause := range ve.Causes {
		errs = append(errs, flattenValidationError(cause)...)
	}
	return errs
}

// leafErrors traduit une erreur feuille en une ou plusieurs ValidationError
// avec chemin. Cas particulier de "missing properties: 'a', 'b'" : le champ
// manquant n'apparaît que dans le message, pas dans InstanceLocation (qui
// pointe l'objet parent) ; on l'ajoute au chemin pour rester précis.
func leafErrors(ve *jsonschema.ValidationError) []error {
	base := instancePath(ve.InstanceLocation)
	if m := missingPropertiesPattern.FindStringSubmatch(ve.Message); m != nil {
		names := quotedNamePattern.FindAllStringSubmatch(m[1], -1)
		if len(names) > 0 {
			errs := make([]error, 0, len(names))
			for _, n := range names {
				errs = append(errs, &ValidationError{Path: joinPath(base, n[1]), Message: "obligatoire"})
			}
			return errs
		}
	}
	return []error{&ValidationError{Path: base, Message: ve.Message}}
}

func instancePath(loc string) string {
	loc = strings.TrimPrefix(loc, "/")
	if loc == "" {
		return ""
	}
	return strings.ReplaceAll(loc, "/", ".")
}
