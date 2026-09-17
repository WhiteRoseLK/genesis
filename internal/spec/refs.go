// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"fmt"
	"regexp"
	"strings"
)

var refPattern = regexp.MustCompile(`^(env|file|vault)://\S+$`)

// validateSecretRefs interdit tout secret littéral : toute clé finissant par
// "_ref" doit être une référence env://, file:// ou vault:// (docs/04-spec.md,
// règle "Aucun secret littéral").
func validateSecretRefs(node any, path string) []error {
	switch v := node.(type) {
	case map[string]any:
		var errs []error
		for key, val := range v {
			childPath := joinPath(path, key)
			if strings.HasSuffix(key, "_ref") {
				s, ok := val.(string)
				if !ok || !refPattern.MatchString(s) {
					errs = append(errs, &ValidationError{
						Path:    childPath,
						Message: "doit être une référence env://, file:// ou vault:// (aucun secret littéral)",
					})
					continue
				}
			}
			errs = append(errs, validateSecretRefs(val, childPath)...)
		}
		return errs
	case []any:
		var errs []error
		for i, item := range v {
			errs = append(errs, validateSecretRefs(item, fmt.Sprintf("%s.%d", path, i))...)
		}
		return errs
	default:
		return nil
	}
}
