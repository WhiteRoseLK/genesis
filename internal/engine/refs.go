// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"os"
	"strings"
)

// resolveRefs remplace chaque clé "*_ref" (docs/04-spec.md : références
// env://, file://, vault:// — la seule exception à "zéro secret fourni",
// doc01 : les identifiants de l'hyperviseur, fournis par référence) par la
// clé sans le suffixe, avec la valeur résolue. Un module ne voit donc jamais
// le schéma de référence, seulement la valeur.
func resolveRefs(m map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			resolved, err := resolveRefs(val)
			if err != nil {
				return nil, err
			}
			out[k] = resolved
		case string:
			if strings.HasSuffix(k, "_ref") {
				resolvedValue, err := resolveRef(val)
				if err != nil {
					return nil, fmt.Errorf("%s : %w", k, err)
				}
				out[strings.TrimSuffix(k, "_ref")] = resolvedValue
				continue
			}
			out[k] = val
		default:
			out[k] = v
		}
	}
	return out, nil
}

func resolveRef(ref string) (string, error) {
	switch {
	case strings.HasPrefix(ref, "env://"):
		name := strings.TrimPrefix(ref, "env://")
		value, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("variable d'environnement %s absente", name)
		}
		return value, nil
	case strings.HasPrefix(ref, "file://"):
		path := strings.TrimPrefix(ref, "file://")
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("lecture de %s : %w", path, err)
		}
		return strings.TrimSpace(string(data)), nil
	case strings.HasPrefix(ref, "vault://"):
		return "", fmt.Errorf("référence vault:// pas encore prise en charge (prévu au jalon J7)")
	default:
		return "", fmt.Errorf("référence %q : schéma non reconnu (attendu env://, file://, vault://)", ref)
	}
}
