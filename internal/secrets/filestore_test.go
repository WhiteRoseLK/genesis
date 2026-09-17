// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"testing"

	"filippo.io/age"
)

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("génération de l'identité de test : %v", err)
	}
	return NewFileStore(t.TempDir(), identity)
}

// TestEnsureIdempotent vérifie le critère d'acceptation J2 : Ensure ne
// régénère jamais un secret déjà présent.
func TestEnsureIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	ref := Ref("pki/root-ca-key")

	calls := 0
	gen := func() (Secret, error) {
		calls++
		return NewSecret("valeur-générée"), nil
	}

	if err := store.Ensure(ctx, ref, gen, Meta{Kind: "private-key"}); err != nil {
		t.Fatalf("premier Ensure : %v", err)
	}
	first, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get après premier Ensure : %v", err)
	}

	if err := store.Ensure(ctx, ref, gen, Meta{Kind: "private-key"}); err != nil {
		t.Fatalf("second Ensure : %v", err)
	}
	second, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get après second Ensure : %v", err)
	}

	if calls != 1 {
		t.Errorf("le générateur a été appelé %d fois, attendu 1 (Ensure doit être idempotent)", calls)
	}
	if first.ExposeSecret() != second.ExposeSecret() {
		t.Errorf("la valeur a changé entre les deux Ensure : %q != %q", first.ExposeSecret(), second.ExposeSecret())
	}
}

func TestPutGetRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	ref := Ref("secrets/kv-approle-secret")

	if err := store.Put(ctx, ref, NewSecret("s3cr3t-value"), Meta{Owner: "secrets", Kind: "token", Recovery: true}); err != nil {
		t.Fatalf("Put : %v", err)
	}

	got, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get : %v", err)
	}
	if got.ExposeSecret() != "s3cr3t-value" {
		t.Errorf("valeur = %q, attendu %q", got.ExposeSecret(), "s3cr3t-value")
	}

	meta, err := store.GetMeta(ctx, ref)
	if err != nil {
		t.Fatalf("GetMeta : %v", err)
	}
	if meta.Owner != "secrets" || meta.Kind != "token" || !meta.Recovery {
		t.Errorf("GetMeta = %+v, attendu owner=secrets kind=token recovery=true", meta)
	}
}

func TestGetMissingRef(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Get(context.Background(), Ref("does/not-exist")); err == nil {
		t.Fatal("Get sur une référence absente : succès inattendu")
	}
}

func TestListReturnsMetaWithoutValue(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.Put(ctx, Ref("pki/root-ca-key"), NewSecret("v1"), Meta{Kind: "private-key"}); err != nil {
		t.Fatalf("Put pki/root-ca-key : %v", err)
	}
	if err := store.Put(ctx, Ref("pki/intermediate-key"), NewSecret("v2"), Meta{Kind: "private-key"}); err != nil {
		t.Fatalf("Put pki/intermediate-key : %v", err)
	}
	if err := store.Put(ctx, Ref("secrets/other"), NewSecret("v3"), Meta{Kind: "token"}); err != nil {
		t.Fatalf("Put secrets/other : %v", err)
	}

	entries, err := store.List(ctx, "pki")
	if err != nil {
		t.Fatalf("List : %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List(\"pki\") a retourné %d entrées, attendu 2 : %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.Meta.Kind != "private-key" {
			t.Errorf("entrée %s : Kind = %q, attendu %q", e.Ref, e.Meta.Kind, "private-key")
		}
	}
}

func TestRefValidationRejectsTraversal(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, bad := range []Ref{"../escape", "pki/../../etc/passwd", "", "/leading", "trailing/", "Has Space"} {
		if err := store.Put(ctx, bad, NewSecret("x"), Meta{}); err == nil {
			t.Errorf("Put(%q) : succès inattendu, référence invalide acceptée", bad)
		}
	}
}
