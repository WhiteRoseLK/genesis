// SPDX-License-Identifier: Apache-2.0

package moduletest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// conformanceFlakyCheckEnv, when set, tells
// TestRunConformance_FailsWhenCheckIsNotRepeatable to run the failing
// scenario for real instead of re-invoking itself (see that test).
const conformanceFlakyCheckEnv = "GENESIS_CONFORMANCE_FLAKY_CHECK"

const minimalManifestYAML = `
apiVersion: genesis/v1alpha1
name: test-conformance
version: 0.0.1
layer: target
`

func writeMinimalManifest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "module.yaml")
	if err := os.WriteFile(path, []byte(minimalManifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// deterministicModule implements the manifest above with a Check that always
// returns the same result, as a well-behaved module must.
type deterministicModule struct {
	modulev1.UnimplementedModuleServer
}

func (deterministicModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return &modulev1.Manifest{Name: "test-conformance", Version: "0.0.1"}, nil
}

func (deterministicModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (deterministicModule) Check(context.Context, *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
}

func TestRunConformance_PassesForADeterministicModule(t *testing.T) {
	RunConformance(t, deterministicModule{}, writeMinimalManifest(t))
}

// flakyCheckModule returns a different status on every other call to Check,
// simulating a module whose Check depends on something besides its input
// (issue #145).
type flakyCheckModule struct {
	modulev1.UnimplementedModuleServer
	calls int
}

func (flakyCheckModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return &modulev1.Manifest{Name: "test-conformance", Version: "0.0.1"}, nil
}

func (flakyCheckModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *flakyCheckModule) Check(context.Context, *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	m.calls++
	if m.calls%2 == 1 {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_TODO}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
}

// TestRunConformance_FailsWhenCheckIsNotRepeatable proves the repeatability
// check actually catches a broken module. RunConformance reports the failure
// on the *testing.T it is given, so proving it fails means letting it fail —
// re-invoking this same test in a subprocess (the standard library's own
// pattern for this, e.g. os/exec's TestHelperProcess) keeps that failure from
// failing the outer `go test` run.
func TestRunConformance_FailsWhenCheckIsNotRepeatable(t *testing.T) {
	if os.Getenv(conformanceFlakyCheckEnv) == "1" {
		RunConformance(t, &flakyCheckModule{}, writeMinimalManifest(t))
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
	cmd.Env = append(os.Environ(), conformanceFlakyCheckEnv+"=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("RunConformance should fail when Check returns a different result on an identical call; subprocess output:\n%s", output)
	}
}
