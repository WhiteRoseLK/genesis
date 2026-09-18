// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"testing"

	"genesis/internal/modulehost"
	"genesis/internal/planner"
	"genesis/internal/resolver"
	"genesis/internal/spec"
)

// TestAddingTestDInsertsAtCorrectPlanPosition est le dernier critère
// d'acceptation du jalon J4 (doc 08) : "ajout d'un module test-d dépendant
// de test-a sans modifier aucun fichier hors test/modules/test-d/ → inséré
// au bon endroit dans le plan."
//
// test-d existe déjà comme fixture sous test/modules/test-d/ (comme
// test-a/b/c) ; ce test prouve que le résolveur, le planificateur et le
// moteur n'ont eu besoin d'aucune modification pour le prendre en charge —
// aucun fichier de ce paquet ne connaît "test-d" par son nom.
func TestAddingTestDInsertsAtCorrectPlanPosition(t *testing.T) {
	searchRoot := t.TempDir()
	installedA := buildAndInstall(t, "test-a", searchRoot)
	installedB := buildAndInstall(t, "test-b", searchRoot)
	installedC := buildAndInstall(t, "test-c", searchRoot)
	installedD := buildAndInstall(t, "test-d", searchRoot)

	env := testEnv()
	env.Capabilities["test-d"] = spec.Capability{Module: "test-d"}

	resolved, err := resolver.Resolve(env, []modulehost.Installed{installedA, installedB, installedC, installedD})
	if err != nil {
		t.Fatalf("Resolve : %v", err)
	}
	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("Build : %v", err)
	}

	if len(plan.Order) != 4 {
		t.Fatalf("Order = %v, attendu 4 modules", plan.Order)
	}
	if indexOf(plan.Order, "test-a") >= indexOf(plan.Order, "test-d") {
		t.Errorf("test-a devrait précéder test-d : %v", plan.Order)
	}
	// test-d ne dépend ni de test-b ni de test-c, et rien ne dépend de lui :
	// sa position relative à eux n'est pas contrainte, seule sa position
	// après test-a compte.

	stateDir := t.TempDir()
	e := New(stateDir, newTestSecretsStore(t))
	if err := e.Run(context.Background(), resolved, plan); err != nil {
		t.Fatalf("Run avec test-d : %v", err)
	}
}
