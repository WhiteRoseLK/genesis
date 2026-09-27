// SPDX-License-Identifier: Apache-2.0

package spec

import "fmt"

// ValidationError reports a structural problem in the spec, with the YAML path
// concerned (M0/M1 acceptance criterion, doc 08).
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
