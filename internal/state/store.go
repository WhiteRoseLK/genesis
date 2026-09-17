// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNotInitialized signale l'absence de state_dir/state.json : `genesis
// init` n'a pas encore été exécuté pour ce state_dir.
var ErrNotInitialized = errors.New("état absent : lancez d'abord `genesis init`")

func statePath(stateDir string) string {
	return filepath.Join(stateDir, "state.json")
}

// Load lit l'état persisté dans state_dir/state.json.
func Load(stateDir string) (*State, error) {
	path := statePath(stateDir)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%s : %w", stateDir, ErrNotInitialized)
	}
	if err != nil {
		return nil, fmt.Errorf("lecture de %s : %w", path, err)
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("état invalide dans %s : %w", path, err)
	}
	return &s, nil
}

// Save persiste s dans state_dir/state.json, en écriture atomique.
func Save(stateDir string, s *State) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("création de %s : %w", stateDir, err)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encodage de l'état : %w", err)
	}
	if err := writeAtomic(statePath(stateDir), raw, 0o600); err != nil {
		return fmt.Errorf("écriture de %s : %w", statePath(stateDir), err)
	}
	return nil
}
