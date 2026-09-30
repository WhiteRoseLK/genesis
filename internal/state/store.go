// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/WhiteRoseLK/genesis/internal/atomicfile"
)

// ErrNotInitialized reports that state_dir/state.json is missing: `genesis
// init` has not been run yet for this state_dir.
var ErrNotInitialized = errors.New("no state: run `genesis init` first")

func statePath(stateDir string) string {
	return filepath.Join(stateDir, "state.json")
}

// Load reads the state persisted in state_dir/state.json.
func Load(stateDir string) (*State, error) {
	path := statePath(stateDir)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%s: %w", stateDir, ErrNotInitialized)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("invalid state in %s: %w", path, err)
	}
	return &s, nil
}

// Save persists s in state_dir/state.json, with an atomic write.
func Save(stateDir string, s *State) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", stateDir, err)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the state: %w", err)
	}
	if err := atomicfile.Write(statePath(stateDir), raw, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", statePath(stateDir), err)
	}
	return nil
}
