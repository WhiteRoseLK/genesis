// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock acquires a non-blocking exclusive lock on state_dir/state.lock. A
// second concurrent call (same process with a different descriptor, or a
// separate process) fails immediately instead of waiting — this is what makes
// a second concurrent `apply` refuse to run (M2 acceptance criterion, doc 08).
func Lock(stateDir string) (release func() error, err error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", stateDir, err)
	}

	path := filepath.Join(stateDir, "state.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("state_dir %s is already locked (is another run in progress?): %w", stateDir, err)
	}

	return func() error {
		defer func() { _ = f.Close() }()
		return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}, nil
}
