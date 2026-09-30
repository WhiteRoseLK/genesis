// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"filippo.io/age"

	"github.com/WhiteRoseLK/genesis/internal/atomicfile"
)

// MasterKeyProvider provides the master key that encrypts the `file` backend
// (docs/06-secrets-state.md, ADR-007). Only the `file` implementation exists
// in iteration 1; the interface exists from now on so that TPM/Shamir/HSM can
// be plugged in later without changing the callers.
type MasterKeyProvider interface {
	// Ensure creates the key if it is missing; never regenerates an existing
	// key. The boolean reports whether the key has just been created (to drive
	// its one-time display by `genesis init`).
	Ensure(ctx context.Context) (identity *age.X25519Identity, created bool, err error)
	// Load loads the existing key; fails if `init` has not been run.
	Load(ctx context.Context) (*age.X25519Identity, error)
}

// FileMasterKeyProvider stores the master key in plaintext on the seed's disk
// (accepted debt, ADR-007) in state_dir/master.key.
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
		return nil, false, fmt.Errorf("creating %s: %w", p.StateDir, err)
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, false, fmt.Errorf("generating the master key: %w", err)
	}

	if err := atomicfile.Write(p.path(), []byte(identity.String()+"\n"), 0o600); err != nil {
		return nil, false, fmt.Errorf("writing the master key: %w", err)
	}

	return identity, true, nil
}

func (p FileMasterKeyProvider) Load(_ context.Context) (*age.X25519Identity, error) {
	existing, err := p.readIfExists()
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("no master key in %s: run `genesis init` first", p.StateDir)
	}
	return existing, nil
}

func (p FileMasterKeyProvider) readIfExists() (*age.X25519Identity, error) {
	raw, err := os.ReadFile(p.path())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", p.path(), err)
	}
	identity, err := age.ParseX25519Identity(trimNewline(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("invalid master key in %s: %w", p.path(), err)
	}
	return identity, nil
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
