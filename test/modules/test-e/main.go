// SPDX-License-Identifier: Apache-2.0

// test-e provides test.e/v1 in a PURE seed phase (no target phase): it
// exercises the cross-module handover between TWO distinct modules
// (docs/05-bootstrap-lifecycle.md, milestone M6) — unlike test-a (which
// provides the same function in both phases, a self-handover), test-e never
// becomes its own target provider: test-f takes over test.e/v1, and it is
// test-f's run (not its own) that triggers its SeedDown.
package main

import (
	"context"
	_ "embed"

	"google.golang.org/grpc"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	echov1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/test/echo/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

type testEModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest
}

func (m *testEModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *testEModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

// Check: compliant once bootstrapped (seeded) or already retired (retired) — a
// pure seed module has nothing else to replay (docs/03 §5).
func (m *testEModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	flags := sdk.StateMap(req.GetState())
	if boolFlag(flags, "seeded") || boolFlag(flags, "retired") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_TODO}, nil
}

func (m *testEModule) SeedUp(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "seeded")
}

func (m *testEModule) Verify(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "verified")
}

// SeedDown: never called by its own plan iteration (test-e provides nothing in
// the target phase) — only by the handover of ANOTHER module (test-f), which
// is precisely what this fixture proves.
func (m *testEModule) SeedDown(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "retired")
}

func (m *testEModule) Destroy(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	s, err := sdk.NewState(map[string]any{})
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func setFlag(req *modulev1.StepRequest, flag string) (*modulev1.StepResult, error) {
	flags := sdk.StateMap(req.GetState())
	flags[flag] = true
	s, err := sdk.NewState(flags)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func boolFlag(flags map[string]any, key string) bool {
	v, _ := flags[key].(bool)
	return v
}

type echoServer struct {
	echov1.UnimplementedEchoServer
}

func (echoServer) Call(_ context.Context, req *echov1.CallRequest) (*echov1.CallResponse, error) {
	return &echov1.CallResponse{Message: "echo: " + req.GetMessage(), From: "test-e", Phase: "seed"}, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	sdk.Serve(&testEModule{manifest: mf.ToProto()}, sdk.FunctionProvider{
		Name:     "test.e/v1",
		Register: func(s *grpc.Server) { echov1.RegisterEchoServer(s, echoServer{}) },
	})
}
