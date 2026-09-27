// SPDX-License-Identifier: Apache-2.0

// Package atomicfile writes files without ever leaving a partially written
// state visible (docs/06-secrets-state.md: "atomic writes"), used by
// internal/secrets, internal/state and internal/modulelock.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write writes data to path through a temporary file followed by a rename.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating a temporary file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op if the rename succeeded

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpPath, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("permissions de %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmpPath, path, err)
	}
	return nil
}
