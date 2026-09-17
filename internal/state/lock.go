// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock acquiert un verrou exclusif non bloquant sur state_dir/state.lock.
// Un deuxième appel concurrent (même processus avec un descripteur
// différent, ou processus distinct) échoue immédiatement au lieu d'attendre
// — c'est ce qui fait refuser un second `apply` concurrent (critère
// d'acceptation du jalon J2, doc 08).
func Lock(stateDir string) (release func() error, err error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("création de %s : %w", stateDir, err)
	}

	path := filepath.Join(stateDir, "state.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("ouverture de %s : %w", path, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("state_dir %s déjà verrouillé (une autre exécution est-elle en cours ?) : %w", stateDir, err)
	}

	return func() error {
		defer func() { _ = f.Close() }()
		return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}, nil
}
