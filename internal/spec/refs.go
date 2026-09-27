// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"fmt"
	"regexp"
	"strings"
)

var refPattern = regexp.MustCompile(`^(env|file|vault)://\S+$`)

// validateSecretRefs forbids any literal secret: every key ending in "_ref"
// must be an env://, file:// or vault:// reference (docs/04-spec.md, rule "No
// literal secret").
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
						Message: "must be an env://, file:// or vault:// reference (no literal secret)",
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
