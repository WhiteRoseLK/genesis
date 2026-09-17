// SPDX-License-Identifier: Apache-2.0

package modulehost

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Fingerprint calcule l'empreinte SHA-256 du binaire d'un module
// (docs/02-architecture.md : vérifiée contre genesis.lock).
func Fingerprint(binaryPath string) (string, error) {
	f, err := os.Open(binaryPath)
	if err != nil {
		return "", fmt.Errorf("ouverture de %s : %w", binaryPath, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("lecture de %s : %w", binaryPath, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
