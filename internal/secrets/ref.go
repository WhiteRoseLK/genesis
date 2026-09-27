// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"fmt"
	"regexp"
	"strings"
)

// Ref is the only secret identifier that leaves the store
// (docs/06-secrets-state.md), e.g. "pki/root-ca-key".
type Ref string

var refSegmentPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Validate checks that the reference is a safe path (no ".." and no characters
// that could be diverted to a file path outside state_dir/secrets/).
func (r Ref) Validate() error {
	s := string(r)
	if s == "" {
		return fmt.Errorf("empty secret reference")
	}
	if strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") {
		return fmt.Errorf("secret reference %q: must neither start nor end with \"/\"", s)
	}
	for _, segment := range strings.Split(s, "/") {
		if !refSegmentPattern.MatchString(segment) {
			return fmt.Errorf("secret reference %q: invalid segment %q (expected [a-z0-9-])", s, segment)
		}
	}
	return nil
}

// Segments splits the reference into path components, once validated.
func (r Ref) Segments() []string {
	return strings.Split(string(r), "/")
}
