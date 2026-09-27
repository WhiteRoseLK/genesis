// SPDX-License-Identifier: Apache-2.0

package state

import "testing"

// TestSecondConcurrentLockRefused is the explicit M2 acceptance criterion (doc
// 08): "two concurrent applies → the second one refuses". flock() locks are
// attached to the open file description, not to the process: two calls to Lock
// with two distinct descriptors faithfully simulate two concurrent runs of
// `genesis apply`, even within a single test process.
func TestSecondConcurrentLockRefused(t *testing.T) {
	dir := t.TempDir()

	release, err := Lock(dir)
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	defer func() { _ = release() }()

	if _, err := Lock(dir); err == nil {
		t.Fatal("concurrent second Lock: unexpected success, it should have been refused")
	}
}

func TestLockReleasedAllowsNextLock(t *testing.T) {
	dir := t.TempDir()

	release, err := Lock(dir)
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	release2, err := Lock(dir)
	if err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	_ = release2()
}
