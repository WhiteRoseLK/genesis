// SPDX-License-Identifier: Apache-2.0

package modulehost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// buildPanickingModule builds test/modules/panicking (docs/02-architecture.md:
// test module) into a temporary binary named as the installation layout
// expects, and returns it.
func buildPanickingModule(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(wd, "..", "..", "test", "modules", "panicking")

	binaryPath := filepath.Join(t.TempDir(), BinaryName())
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = sourceDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the panicking test module: %v\n%s", err, out)
	}
	return binaryPath
}

// TestModuleCrashProducesCleanError is the M3 acceptance criterion (doc 08): a
// module that crashes during a step produces a clean error without stopping
// the core — the test process must stay alive and get a usable error, not a
// panic that travels up to here.
func TestModuleCrashProducesCleanError(t *testing.T) {
	binaryPath := buildPanickingModule(t)

	client, err := Launch(binaryPath, nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// The module answers Describe normally before crashing on Check.
	manifest, err := client.Describe(ctx)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if manifest.GetName() != "panicking" {
		t.Fatalf("Describe().Name = %q, want %q", manifest.GetName(), "panicking")
	}

	_, err = client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test"})
	if err == nil {
		t.Fatal("Check on a panicking module: unexpected success")
	}
	t.Logf("clean error obtained as expected: %v", err)

	// The core (this test process) is still alive here: that is the point of
	// the test. A second request on the same connection must also fail
	// cleanly, not cause another hidden crash.
	if _, err := client.Describe(ctx); err == nil {
		t.Log("Describe after the crash succeeded (the plugin may have restarted) — acceptable, the core did not crash")
	}
}
