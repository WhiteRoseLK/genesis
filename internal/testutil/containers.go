// SPDX-License-Identifier: Apache-2.0

// Package testutil groups the helpers shared by the tests.
package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/WhiteRoseLK/genesis/internal/runner"
)

// RequireRuntime returns the machine's container runtime and fails the test if
// no daemon is reachable. The tests that call it carry the `docker` build tag
// (make test-docker): running them without a daemon is an environment error,
// not a reason to skip them.
func RequireRuntime(t testing.TB) *runner.ContainerRuntime {
	t.Helper()
	rt, err := runner.DetectContainerRuntime("auto")
	if err != nil {
		t.Fatalf("tests `docker`: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rt.Ping(ctx); err != nil {
		t.Fatalf("`docker` tests: container daemon unreachable (start docker or podman): %v", err)
	}
	return rt
}
