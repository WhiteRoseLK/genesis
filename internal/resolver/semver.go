// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"fmt"
	"strconv"
	"strings"
)

// checkCoreCompatibility vérifie que CoreVersion satisfait constraint, au
// format doc03 (ex. ">=0.1.0 <0.2.0") : une liste de conditions
// ">=X.Y.Z" / "<X.Y.Z" séparées par des espaces, toutes requises.
func checkCoreCompatibility(moduleName, constraint string) error {
	if constraint == "" {
		return nil
	}
	current, err := parseVersion(CoreVersion)
	if err != nil {
		return fmt.Errorf("version du cœur %q invalide : %w", CoreVersion, err)
	}

	for _, cond := range strings.Fields(constraint) {
		op, verStr, err := splitCondition(cond)
		if err != nil {
			return fmt.Errorf("module %q : contrainte core %q invalide : %w", moduleName, constraint, err)
		}
		want, err := parseVersion(verStr)
		if err != nil {
			return fmt.Errorf("module %q : contrainte core %q invalide : %w", moduleName, constraint, err)
		}
		cmp := compareVersions(current, want)
		ok := false
		switch op {
		case ">=":
			ok = cmp >= 0
		case "<":
			ok = cmp < 0
		case ">":
			ok = cmp > 0
		case "<=":
			ok = cmp <= 0
		case "==", "=":
			ok = cmp == 0
		}
		if !ok {
			return fmt.Errorf("module %q : incompatible avec le cœur %s (contrainte %q)", moduleName, CoreVersion, constraint)
		}
	}
	return nil
}

func splitCondition(cond string) (op, version string, err error) {
	for _, candidate := range []string{">=", "<=", "==", ">", "<", "="} {
		if strings.HasPrefix(cond, candidate) {
			return candidate, strings.TrimPrefix(cond, candidate), nil
		}
	}
	return "", "", fmt.Errorf("opérateur manquant dans %q", cond)
}

type version [3]int

func parseVersion(s string) (version, error) {
	parts := strings.SplitN(s, ".", 3)
	if len(parts) != 3 {
		return version{}, fmt.Errorf("format attendu X.Y.Z, reçu %q", s)
	}
	var v version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return version{}, fmt.Errorf("composant %q non numérique dans %q", p, s)
		}
		v[i] = n
	}
	return v, nil
}

func compareVersions(a, b version) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
