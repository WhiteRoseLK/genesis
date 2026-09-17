// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"filippo.io/age"

	"genesis/internal/atomicfile"
)

// MasterKeyProvider fournit la clé maîtresse qui chiffre le backend `file`
// (docs/06-secrets-etat.md, ADR-007). Seule l'implémentation `file` existe
// à l'itération 1 ; l'interface existe dès maintenant pour brancher
// TPM/Shamir/HSM plus tard sans changer les appelants.
type MasterKeyProvider interface {
	// Ensure crée la clé si elle est absente ; ne régénère jamais une clé
	// existante. Le booléen indique si la clé vient d'être créée (pour
	// piloter son affichage unique par `genesis init`).
	Ensure(ctx context.Context) (identity *age.X25519Identity, created bool, err error)
	// Load charge la clé existante ; échoue si `init` n'a pas été exécuté.
	Load(ctx context.Context) (*age.X25519Identity, error)
}

// FileMasterKeyProvider stocke la clé maîtresse en clair sur le disque de la
// graine (dette assumée, ADR-007) dans state_dir/master.key.
type FileMasterKeyProvider struct {
	StateDir string
}

func (p FileMasterKeyProvider) path() string {
	return filepath.Join(p.StateDir, "master.key")
}

func (p FileMasterKeyProvider) Ensure(_ context.Context) (*age.X25519Identity, bool, error) {
	if existing, err := p.readIfExists(); err != nil {
		return nil, false, err
	} else if existing != nil {
		return existing, false, nil
	}

	if err := os.MkdirAll(p.StateDir, 0o700); err != nil {
		return nil, false, fmt.Errorf("création de %s : %w", p.StateDir, err)
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, false, fmt.Errorf("génération de la clé maîtresse : %w", err)
	}

	if err := atomicfile.Write(p.path(), []byte(identity.String()+"\n"), 0o600); err != nil {
		return nil, false, fmt.Errorf("écriture de la clé maîtresse : %w", err)
	}

	return identity, true, nil
}

func (p FileMasterKeyProvider) Load(_ context.Context) (*age.X25519Identity, error) {
	existing, err := p.readIfExists()
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("aucune clé maîtresse dans %s : lancez d'abord `genesis init`", p.StateDir)
	}
	return existing, nil
}

func (p FileMasterKeyProvider) readIfExists() (*age.X25519Identity, error) {
	raw, err := os.ReadFile(p.path())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lecture de %s : %w", p.path(), err)
	}
	identity, err := age.ParseX25519Identity(trimNewline(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("clé maîtresse invalide dans %s : %w", p.path(), err)
	}
	return identity, nil
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
