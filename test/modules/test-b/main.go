// SPDX-License-Identifier: Apache-2.0

// test-b requires test.a/v1 and provides test.b/v1 (docs/08-milestones.md,
// M4): it checks that the broker routes a call made during a real step.
package main

import (
	"context"
	_ "embed"
	"fmt"

	"google.golang.org/grpc"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	echov1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/test/echo/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

type testBModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest
	broker   *sdk.BrokerClient
}

func (m *testBModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *testBModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *testBModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *testBModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	flags := sdk.StateMap(req.GetState())
	if boolFlag(flags, "verified") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_TODO}, nil
}

func (m *testBModule) Provision(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "provisioned")
}

func (m *testBModule) Configure(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "configured")
}

// Verify really calls test.a/v1 through the broker, during a step — the
// complete round trip milestone M4 must prove.
func (m *testBModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	conn, err := m.broker.Dial(req.GetBrokerToken())
	if err != nil {
		return nil, fmt.Errorf("connecting to the broker: %w", err)
	}
	resp, err := echov1.NewEchoClient(conn).Call(ctx, &echov1.CallRequest{Message: "hello from test-b"})
	if err != nil {
		return nil, fmt.Errorf("calling test.a/v1 through the broker: %w", err)
	}
	if resp.GetFrom() != "test-a" {
		return nil, fmt.Errorf("unexpected response from test.a/v1: %+v", resp)
	}
	return setFlag(req, "verified")
}

func (m *testBModule) Destroy(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
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

// echoServer implements functions/test/echo/v1 for the test.b/v1 function.
type echoServer struct {
	echov1.UnimplementedEchoServer
}

func (echoServer) Call(_ context.Context, req *echov1.CallRequest) (*echov1.CallResponse, error) {
	return &echov1.CallResponse{Message: "echo: " + req.GetMessage(), From: "test-b", Phase: "target"}, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	sdk.Serve(&testBModule{manifest: mf.ToProto()}, sdk.FunctionProvider{
		Name:     "test.b/v1",
		Register: func(s *grpc.Server) { echov1.RegisterEchoServer(s, echoServer{}) },
	})
}
