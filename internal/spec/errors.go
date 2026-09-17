// SPDX-License-Identifier: Apache-2.0

package spec

import "fmt"

// ValidationError signale un problème de structure dans la spec, avec le
// chemin YAML concerné (critère d'acceptation du jalon J0/J1, doc 08).
type ValidationError struct {
	Path    string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

func joinPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}
