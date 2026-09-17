// SPDX-License-Identifier: Apache-2.0

package state

import "testing"

// TestSecondConcurrentLockRefused est le critère d'acceptation explicite du
// jalon J2 (doc 08) : "deux apply concurrents → le second refuse". Les
// verrous flock() sont attachés à la description de fichier ouverte, pas au
// processus : deux appels à Lock avec deux descripteurs distincts simulent
// fidèlement deux exécutions concurrentes de `genesis apply` même dans un
// seul process de test.
func TestSecondConcurrentLockRefused(t *testing.T) {
	dir := t.TempDir()

	release, err := Lock(dir)
	if err != nil {
		t.Fatalf("premier Lock : %v", err)
	}
	defer func() { _ = release() }()

	if _, err := Lock(dir); err == nil {
		t.Fatal("second Lock concurrent : succès inattendu, il aurait dû être refusé")
	}
}

func TestLockReleasedAllowsNextLock(t *testing.T) {
	dir := t.TempDir()

	release, err := Lock(dir)
	if err != nil {
		t.Fatalf("premier Lock : %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release : %v", err)
	}

	release2, err := Lock(dir)
	if err != nil {
		t.Fatalf("Lock après release : %v", err)
	}
	_ = release2()
}
