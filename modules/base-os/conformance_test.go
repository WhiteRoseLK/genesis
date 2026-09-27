// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	"github.com/WhiteRoseLK/genesis/sdk/go/moduletest"
)

// TestConformance : suite de conformité du SDK (docs/10-adding-a-module.md,
// étape 5 ; critère d'acceptation du jalon J5, doc 08 : "conformité SDK verte").
func TestConformance(t *testing.T) {
	mf, err := sdk.LoadManifest("module.yaml")
	if err != nil {
		t.Fatalf("chargement de module.yaml : %v", err)
	}
	moduletest.RunConformance(t, &baseOSModule{manifest: mf.ToProto()}, "module.yaml")
}
