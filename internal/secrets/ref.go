// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"fmt"
	"regexp"
	"strings"
)

// Ref est le seul identifiant de secret circulant hors du store
// (docs/06-secrets-state.md), ex. "pki/root-ca-key".
type Ref string

var refSegmentPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Validate vérifie que la référence est un chemin sûr (pas de ".." ni de
// caractères susceptibles d'être détournés vers un chemin de fichier hors
// de state_dir/secrets/).
func (r Ref) Validate() error {
	s := string(r)
	if s == "" {
		return fmt.Errorf("référence de secret vide")
	}
	if strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") {
		return fmt.Errorf("référence de secret %q : ne doit pas commencer ni finir par \"/\"", s)
	}
	for _, segment := range strings.Split(s, "/") {
		if !refSegmentPattern.MatchString(segment) {
			return fmt.Errorf("référence de secret %q : segment %q invalide (attendu [a-z0-9-])", s, segment)
		}
	}
	return nil
}

// Segments découpe la référence en composants de chemin, une fois validée.
func (r Ref) Segments() []string {
	return strings.Split(string(r), "/")
}
