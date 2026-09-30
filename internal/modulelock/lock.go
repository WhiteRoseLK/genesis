// SPDX-License-Identifier: Apache-2.0

// Package modulelock reads and writes genesis.lock: name, version and SHA-256
// digest of each module used, next to the spec (docs/02-architecture.md).
package modulelock

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/WhiteRoseLK/genesis/internal/atomicfile"
)

// Entry pins the version and digest of a module.
type Entry struct {
	Version string `yaml:"version"`
	SHA256  string `yaml:"sha256"`
}

// Lock is the content of genesis.lock.
type Lock struct {
	Modules map[string]Entry `yaml:"modules"`
}

// Load reads path, or returns an empty Lock if the file does not exist yet
// (first `genesis modules install`).
func Load(path string) (*Lock, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Lock{Modules: map[string]Entry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var l Lock
	if err := yaml.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("%s: invalid YAML: %w", path, err)
	}
	if l.Modules == nil {
		l.Modules = map[string]Entry{}
	}
	return &l, nil
}

// Save persists the lock with an atomic write. This file is not secret: it is
// meant to be committed (reproducibility, doc 02).
func (l *Lock) Save(path string) error {
	raw, err := yaml.Marshal(l)
	if err != nil {
		return fmt.Errorf("encodage de %s: %w", path, err)
	}
	if err := atomicfile.Write(path, raw, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// Verify compares the given digest with the one pinned in the lock for name.
// Error if the module is not in the lock, or if the digest differs (M3
// acceptance criterion, doc 08).
func (l *Lock) Verify(name, version, sha256 string) error {
	entry, ok := l.Modules[name]
	if !ok {
		return fmt.Errorf("module %q: missing from genesis.lock (run `genesis modules install`)", name)
	}
	if entry.Version != version {
		return fmt.Errorf("module %q: version %s installed, genesis.lock expects %s", name, version, entry.Version)
	}
	if entry.SHA256 != sha256 {
		return fmt.Errorf("module %q: digest %s does not match genesis.lock (%s) — modified or compromised binary?", name, sha256, entry.SHA256)
	}
	return nil
}
