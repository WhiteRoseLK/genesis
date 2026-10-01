// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"errors"
	"testing"

	"filippo.io/age"
)

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generating the test identity: %v", err)
	}
	return NewFileStore(t.TempDir(), identity)
}

// TestEnsureIdempotent checks the M2 acceptance criterion: Ensure never
// regenerates a secret that is already present.
func TestEnsureIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	ref := Ref("pki/root-ca-key")

	calls := 0
	gen := func() (Secret, error) {
		calls++
		return NewSecret("generated-value"), nil
	}

	if err := store.Ensure(ctx, ref, gen, Meta{Kind: "private-key"}); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	first, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get after the first Ensure: %v", err)
	}

	if err := store.Ensure(ctx, ref, gen, Meta{Kind: "private-key"}); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	second, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get after the second Ensure: %v", err)
	}

	if calls != 1 {
		t.Errorf("the generator was called %d times, want 1 (Ensure must be idempotent)", calls)
	}
	if first.ExposeSecret() != second.ExposeSecret() {
		t.Errorf("the value changed between the two Ensure calls: %q != %q", first.ExposeSecret(), second.ExposeSecret())
	}
}

func TestPutGetRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	ref := Ref("secrets/kv-approle-secret")

	if err := store.Put(ctx, ref, NewSecret("s3cr3t-value"), Meta{Owner: "secrets", Kind: "token", Recovery: true}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ExposeSecret() != "s3cr3t-value" {
		t.Errorf("value = %q, want %q", got.ExposeSecret(), "s3cr3t-value")
	}

	meta, err := store.GetMeta(ctx, ref)
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if meta.Owner != "secrets" || meta.Kind != "token" || !meta.Recovery {
		t.Errorf("GetMeta = %+v, want owner=secrets kind=token recovery=true", meta)
	}
}

func TestGetMissingRef(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Get(context.Background(), Ref("does/not-exist")); err == nil {
		t.Fatal("Get on a missing reference: unexpected success")
	}
}

func TestListReturnsMetaWithoutValue(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.Put(ctx, Ref("pki/root-ca-key"), NewSecret("v1"), Meta{Kind: "private-key"}); err != nil {
		t.Fatalf("Put pki/root-ca-key: %v", err)
	}
	if err := store.Put(ctx, Ref("pki/intermediate-key"), NewSecret("v2"), Meta{Kind: "private-key"}); err != nil {
		t.Fatalf("Put pki/intermediate-key: %v", err)
	}
	if err := store.Put(ctx, Ref("secrets/other"), NewSecret("v3"), Meta{Kind: "token"}); err != nil {
		t.Fatalf("Put secrets/other: %v", err)
	}

	entries, err := store.List(ctx, "pki")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List(\"pki\") returned %d entries, want 2: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.Meta.Kind != "private-key" {
			t.Errorf("entry %s: Kind = %q, want %q", e.Ref, e.Meta.Kind, "private-key")
		}
	}
}

func TestRefValidationRejectsTraversal(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, bad := range []Ref{"../escape", "pki/../../etc/passwd", "", "/leading", "trailing/", "Has Space"} {
		if err := store.Put(ctx, bad, NewSecret("x"), Meta{}); err == nil {
			t.Errorf("Put(%q): unexpected success, invalid reference accepted", bad)
		}
	}
}

func TestFileStoreReadOnly(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Write a recovery secret before switching to read-only.
	recoveryRef := Ref("ca/root-key")
	if err := store.Put(ctx, recoveryRef, NewSecret("root-key-content"), Meta{Kind: "private-key", Recovery: true}); err != nil {
		t.Fatalf("Put recovery secret: %v", err)
	}

	store.SetReadOnly(true)
	if !store.ReadOnly() {
		t.Errorf("ReadOnly = false, want true")
	}

	// 1. Reading existing secret must succeed.
	got, err := store.Get(ctx, recoveryRef)
	if err != nil {
		t.Fatalf("Get recovery secret in read-only mode: %v", err)
	}
	if got.ExposeSecret() != "root-key-content" {
		t.Errorf("value = %q, want %q", got.ExposeSecret(), "root-key-content")
	}

	// 2. Reading metadata must succeed.
	meta, err := store.GetMeta(ctx, recoveryRef)
	if err != nil {
		t.Fatalf("GetMeta in read-only mode: %v", err)
	}
	if !meta.Recovery {
		t.Errorf("Recovery = false, want true")
	}

	// 3. Ensure on already-existing secret must succeed (idempotent read).
	calls := 0
	err = store.Ensure(ctx, recoveryRef, func() (Secret, error) {
		calls++
		return NewSecret("regenerated"), nil
	}, Meta{Kind: "private-key", Recovery: true})
	if err != nil {
		t.Fatalf("Ensure on existing secret in read-only mode: %v", err)
	}
	if calls != 0 {
		t.Errorf("generator called %d times on existing secret", calls)
	}

	// 4. Put must fail with ErrReadOnly.
	newRef := Ref("app/new-secret")
	if err := store.Put(ctx, newRef, NewSecret("val"), Meta{Kind: "password"}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Put in read-only mode = %v, want %v", err, ErrReadOnly)
	}

	// 5. Ensure on missing secret must fail with ErrReadOnly without generating.
	genCalled := false
	err = store.Ensure(ctx, newRef, func() (Secret, error) {
		genCalled = true
		return NewSecret("val"), nil
	}, Meta{Kind: "password"})
	if !errors.Is(err, ErrReadOnly) {
		t.Errorf("Ensure on missing secret in read-only mode = %v, want %v", err, ErrReadOnly)
	}
	if genCalled {
		t.Errorf("generator was called for missing secret in read-only mode")
	}
}
