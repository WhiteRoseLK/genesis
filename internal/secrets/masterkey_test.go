// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"testing"
)

func TestMasterKeyEnsureIdempotent(t *testing.T) {
	dir := t.TempDir()
	provider := FileMasterKeyProvider{StateDir: dir}
	ctx := context.Background()

	first, created, err := provider.Ensure(ctx)
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if !created {
		t.Error("first Ensure: created = false, want true (first generation)")
	}

	second, created, err := provider.Ensure(ctx)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if created {
		t.Error("second Ensure: created = true, want false (key already present)")
	}
	if first.String() != second.String() {
		t.Error("the master key changed between the two Ensure calls")
	}
}

func TestMasterKeyLoadWithoutInit(t *testing.T) {
	provider := FileMasterKeyProvider{StateDir: t.TempDir()}
	if _, err := provider.Load(context.Background()); err == nil {
		t.Fatal("Load without a prior init: unexpected success")
	}
}
