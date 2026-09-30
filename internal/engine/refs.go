// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"os"
	"strings"
)

// resolveRefs replaces each "*_ref" key (docs/04-spec.md: env://, file://,
// vault:// references — the only exception to "zero secrets provided",
// doc 01: the hypervisor credentials, provided by reference) with the key
// without the suffix, holding the resolved value. A module therefore never
// sees the reference scheme, only the value.
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
					return nil, fmt.Errorf("%s: %w", k, err)
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
			return "", fmt.Errorf("environment variable %s is not set", name)
		}
		return value, nil
	case strings.HasPrefix(ref, "file://"):
		path := strings.TrimPrefix(ref, "file://")
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", path, err)
		}
		return strings.TrimSpace(string(data)), nil
	case strings.HasPrefix(ref, "vault://"):
		return "", fmt.Errorf("vault:// references are not supported yet")
	default:
		return "", fmt.Errorf("reference %q: unknown scheme (expected env://, file://, vault://)", ref)
	}
}
