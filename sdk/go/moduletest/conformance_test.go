// SPDX-License-Identifier: Apache-2.0

package moduletest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

const (
	minimalManifestYAML = `
apiVersion: genesis/v1alpha1
name: test-conformance
version: 0.0.1
layer: target
`
	// expectFailureEnv makes a test run its scenario for real instead of
	// re-invoking itself (see expectConformanceFailure).
	expectFailureEnv = "GENESIS_CONFORMANCE_EXPECT_FAILURE"
)

func writeMinimalManifest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "module.yaml")
	if err := os.WriteFile(path, []byte(minimalManifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// expectConformanceFailure runs RunConformance on impl and requires it to
// fail. RunConformance reports on the *testing.T it is given, so letting it
// fail means re-invoking the calling test in a subprocess (the pattern of
// os/exec's own tests), which keeps the expected failure out of this run.
func expectConformanceFailure(t *testing.T, impl modulev1.ModuleServer) {
	t.Helper()
	if os.Getenv(expectFailureEnv) == "1" {
		RunConformance(t, impl, writeMinimalManifest(t))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
	cmd.Env = append(os.Environ(), expectFailureEnv+"=1")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("RunConformance should have failed; subprocess output:\n%s", out)
	}
}

// wellBehavedModule satisfies every check of the suite.
type wellBehavedModule struct {
	modulev1.UnimplementedModuleServer
}

func (wellBehavedModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return &modulev1.Manifest{Name: "test-conformance", Version: "0.0.1"}, nil
}

func (wellBehavedModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (wellBehavedModule) Check(context.Context, *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
}

func TestRunConformance_PassesForAWellBehavedModule(t *testing.T) {
	RunConformance(t, wellBehavedModule{}, writeMinimalManifest(t))
}

// leakyValidateModule echoes a config value into a diagnostic message, as a
// module that formats the offending value into its error would.
type leakyValidateModule struct {
	wellBehavedModule
}

func (leakyValidateModule) Validate(_ context.Context, req *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{Diagnostics: []*modulev1.Diagnostic{{
		Path:     "password",
		Message:  fmt.Sprintf("invalid password %q", req.GetConfig().GetFields()["password"].GetStringValue()),
		Severity: "error",
	}}}, nil
}

func TestRunConformance_FailsWhenValidateLeaksASecret(t *testing.T) {
	expectConformanceFailure(t, leakyValidateModule{})
}

// leakyCheckModule puts a config value in the detail of its Check result.
type leakyCheckModule struct {
	wellBehavedModule
}

func (leakyCheckModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	return &modulev1.CheckResult{
		Status: modulev1.CheckResult_STATUS_TODO,
		Detail: "token " + req.GetConfig().GetFields()["token"].GetStringValue(),
	}, nil
}

func TestRunConformance_FailsWhenCheckLeaksASecret(t *testing.T) {
	expectConformanceFailure(t, leakyCheckModule{})
}

// leakyCheckErrorModule returns an error that quotes a config value.
type leakyCheckErrorModule struct {
	wellBehavedModule
}

func (leakyCheckErrorModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	return nil, fmt.Errorf("connecting with api_key %s failed", req.GetConfig().GetFields()["api_key"].GetStringValue())
}

func TestRunConformance_FailsWhenCheckErrorLeaksASecret(t *testing.T) {
	expectConformanceFailure(t, leakyCheckErrorModule{})
}
