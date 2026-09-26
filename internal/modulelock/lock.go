// SPDX-License-Identifier: Apache-2.0

// Package modulelock lit et écrit genesis.lock : nom, version et empreinte
// SHA-256 de chaque module utilisé, à côté de la spec
// (docs/02-architecture.md).
package modulelock

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/WhiteRoseLK/genesis/internal/atomicfile"
)

// Entry fige la version et l'empreinte d'un module.
type Entry struct {
	Version string `yaml:"version"`
	SHA256  string `yaml:"sha256"`
}

// Lock est le contenu de genesis.lock.
type Lock struct {
	Modules map[string]Entry `yaml:"modules"`
}

// Load lit path, ou renvoie un Lock vide si le fichier n'existe pas encore
// (premier `genesis modules install`).
func Load(path string) (*Lock, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Lock{Modules: map[string]Entry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lecture de %s : %w", path, err)
	}
	var l Lock
	if err := yaml.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("%s : YAML invalide : %w", path, err)
	}
	if l.Modules == nil {
		l.Modules = map[string]Entry{}
	}
	return &l, nil
}

// Save persiste le lock, en écriture atomique. Ce fichier n'est pas secret :
// il est fait pour être committé (reproductibilité, doc 02).
func (l *Lock) Save(path string) error {
	raw, err := yaml.Marshal(l)
	if err != nil {
		return fmt.Errorf("encodage de %s : %w", path, err)
	}
	if err := atomicfile.Write(path, raw, 0o644); err != nil {
		return fmt.Errorf("écriture de %s : %w", path, err)
	}
	return nil
}

// Verify compare l'empreinte fournie à celle figée dans le lock pour name.
// Erreur si le module n'est pas dans le lock, ou si l'empreinte diverge
// (critère d'acceptation du jalon J3, doc 08).
func (l *Lock) Verify(name, version, sha256 string) error {
	entry, ok := l.Modules[name]
	if !ok {
		return fmt.Errorf("module %q : absent de genesis.lock (lancez `genesis modules install`)", name)
	}
	if entry.Version != version {
		return fmt.Errorf("module %q : version %s installée, genesis.lock attend %s", name, version, entry.Version)
	}
	if entry.SHA256 != sha256 {
		return fmt.Errorf("module %q : empreinte %s ne correspond pas à genesis.lock (%s) — binaire modifié ou compromis ?", name, sha256, entry.SHA256)
	}
	return nil
}
