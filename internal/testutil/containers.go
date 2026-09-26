// SPDX-License-Identifier: Apache-2.0

// Package testutil regroupe les aides partagées par les tests.
package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/WhiteRoseLK/genesis/internal/runner"
)

// RequireRuntime renvoie le runtime de conteneurs de la machine et fait
// échouer le test si aucun démon n'est joignable. Les tests qui l'appellent
// portent le build tag `docker` (make test-docker) : les lancer sans démon
// est une erreur de l'environnement, pas une raison de les ignorer.
func RequireRuntime(t testing.TB) *runner.ContainerRuntime {
	t.Helper()
	rt, err := runner.DetectContainerRuntime("auto")
	if err != nil {
		t.Fatalf("tests `docker` : %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rt.Ping(ctx); err != nil {
		t.Fatalf("tests `docker` : démon de conteneurs injoignable (démarrer docker ou podman) : %v", err)
	}
	return rt
}
