// SPDX-License-Identifier: Apache-2.0

// Package moduletest est le harnais de test du SDK : suite de conformité
// qu'un module doit passer avant d'être considéré terminé
// (docs/10-ajouter-un-module.md, docs/03-contrat-module.md §4).
//
// Squelette au jalon J3 : seules les vérifications qui ne dépendent pas du
// broker sont faites ici (Describe cohérent avec le manifest, Validate ne
// plante pas). Les vérifications sémantiques complètes — idempotence
// rejouée, absence de secret dans les sorties, respect de chaque fonction
// fournie avec des fonctions requises simulées — ont besoin du broker et
// arrivent au jalon J4 (docs/08-jalons.md).
package moduletest

import (
	"context"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	sdk "genesis/sdk/go"
	modulev1 "genesis/sdk/go/gen/module/v1"
)

// RunConformance exécute la suite de conformité minimale sur impl, dont le
// manifest est à manifestPath (docs/10-ajouter-un-module.md, étape 5 :
// `go test ./... -run Conformance`).
func RunConformance(t *testing.T, impl modulev1.ModuleServer, manifestPath string) {
	t.Helper()
	ctx := context.Background()

	manifest, err := sdk.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("chargement de %s : %v", manifestPath, err)
	}
	want := manifest.ToProto()

	got, err := impl.Describe(ctx, &modulev1.Empty{})
	if err != nil {
		t.Fatalf("Describe : %v", err)
	}
	if got.GetName() != want.GetName() {
		t.Errorf("Describe().Name = %q, attendu %q (module.yaml)", got.GetName(), want.GetName())
	}
	if got.GetVersion() != want.GetVersion() {
		t.Errorf("Describe().Version = %q, attendu %q (module.yaml)", got.GetVersion(), want.GetVersion())
	}
	if len(got.GetProvides()) != len(want.GetProvides()) {
		t.Errorf("Describe().Provides a %d entrée(s), attendu %d (module.yaml)", len(got.GetProvides()), len(want.GetProvides()))
	}

	emptyConfig, err := structpb.NewStruct(map[string]any{})
	if err != nil {
		t.Fatalf("construction d'une config vide : %v", err)
	}
	if _, err := impl.Validate(ctx, &modulev1.ValidateRequest{Config: emptyConfig}); err != nil {
		t.Errorf("Validate avec une config vide a renvoyé une erreur de transport plutôt que des Diagnostics : %v", err)
	}
}
