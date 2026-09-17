// SPDX-License-Identifier: Apache-2.0

// Package atomicfile écrit des fichiers sans jamais laisser un état
// partiellement écrit visible (docs/06-secrets-etat.md : "écritures atomiques"),
// utilisé par internal/secrets, internal/state et internal/modulelock.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write écrit data dans path via un fichier temporaire suivi d'un rename.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("création du fichier temporaire dans %s : %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op si le rename a réussi

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("écriture de %s : %w", tmpPath, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("permissions de %s : %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("fermeture de %s : %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renommage de %s vers %s : %w", tmpPath, path, err)
	}
	return nil
}
