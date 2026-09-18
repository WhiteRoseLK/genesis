// SPDX-License-Identifier: Apache-2.0

// test-a est la racine de la chaîne test-a -> test-b -> test-c
// (docs/08-jalons.md, J4) : il fournit test.a/v1 en phase graine ET cible,
// exerçant SeedUp/Handover/SeedDown (docs/03-contrat-module.md §5).
package main

import (
	"context"
	_ "embed"

	"google.golang.org/grpc"

	sdk "genesis/sdk/go"
	echov1 "genesis/sdk/go/gen/functions/test/echo/v1"
	modulev1 "genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

type testAModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest
}

func (m *testAModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *testAModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *testAModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	flags := sdk.StateMap(req.GetState())
	if boolFlag(flags, "handed_over") && boolFlag(flags, "seed_down") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_CONFORME}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_A_FAIRE}, nil
}

func (m *testAModule) SeedUp(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "seeded")
}

func (m *testAModule) Provision(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "provisioned")
}

func (m *testAModule) Configure(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "configured")
}

func (m *testAModule) Verify(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "verified")
}

func (m *testAModule) Handover(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "handed_over")
}

func (m *testAModule) SeedDown(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "seed_down")
}

func (m *testAModule) Destroy(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
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

// echoServer implémente functions/test/echo/v1 pour la fonction test.a/v1.
type echoServer struct {
	echov1.UnimplementedEchoServer
}

func (echoServer) Call(_ context.Context, req *echov1.CallRequest) (*echov1.CallResponse, error) {
	return &echov1.CallResponse{Message: "echo: " + req.GetMessage(), From: "test-a", Phase: "target"}, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	sdk.Serve(&testAModule{manifest: mf.ToProto()}, sdk.FunctionProvider{
		Name:     "test.a/v1",
		Register: func(s *grpc.Server) { echov1.RegisterEchoServer(s, echoServer{}) },
	})
}
