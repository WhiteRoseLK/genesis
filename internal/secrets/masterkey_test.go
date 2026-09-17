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
		t.Fatalf("premier Ensure : %v", err)
	}
	if !created {
		t.Error("premier Ensure : created = false, attendu true (première génération)")
	}

	second, created, err := provider.Ensure(ctx)
	if err != nil {
		t.Fatalf("second Ensure : %v", err)
	}
	if created {
		t.Error("second Ensure : created = true, attendu false (clé déjà présente)")
	}
	if first.String() != second.String() {
		t.Error("la clé maîtresse a changé entre les deux Ensure")
	}
}

func TestMasterKeyLoadWithoutInit(t *testing.T) {
	provider := FileMasterKeyProvider{StateDir: t.TempDir()}
	if _, err := provider.Load(context.Background()); err == nil {
		t.Fatal("Load sans init préalable : succès inattendu")
	}
}
