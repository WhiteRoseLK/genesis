// SPDX-License-Identifier: Apache-2.0

// test-f takes over test.e/v1 in the target phase, bootstrapped by test-e in
// the seed phase (docs/05-bootstrap-lifecycle.md, milestone M6): Handover
// really reads test.e/v1@seed through the broker (two distinct modules, unlike
// test-a, which takes over from itself) — the core (internal/engine) must then
// trigger SeedDown on test-e, not on test-f.
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

type testFModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest
	broker   *sdk.BrokerClient
}

func (m *testFModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *testFModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *testFModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *testFModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	flags := sdk.StateMap(req.GetState())
	if boolFlag(flags, "handed_over") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_TODO}, nil
}

func (m *testFModule) Provision(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "provisioned")
}

func (m *testFModule) Configure(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "configured")
}

func (m *testFModule) Verify(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "verified")
}

// Handover really reads test.e/v1@seed (dialling with THIS call's token, not a
// cached token — docs/02-architecture.md: one session per call) and keeps the
// proof of the read in the state.
func (m *testFModule) Handover(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	conn, err := m.broker.Dial(req.GetBrokerToken())
	if err != nil {
		return nil, fmt.Errorf("connecting to the broker: %w", err)
	}
	resp, err := echov1.NewEchoClient(conn).Call(ctx, &echov1.CallRequest{Message: "handover from test-f"})
	if err != nil {
		return nil, fmt.Errorf("calling test.e/v1@seed through the broker: %w", err)
	}
	if resp.GetFrom() != "test-e" {
		return nil, fmt.Errorf("unexpected response from test.e/v1@seed: %+v", resp)
	}
	flags := sdk.StateMap(req.GetState())
	flags["handed_over"] = true
	flags["handover_source"] = resp.GetFrom()
	s, err := sdk.NewState(flags)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func (m *testFModule) Destroy(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
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

// echoServer implements functions/test/echo/v1 for test.e/v1 in the target
// phase (provided BY test-f, once the handover is done).
type echoServer struct {
	echov1.UnimplementedEchoServer
}

func (echoServer) Call(_ context.Context, req *echov1.CallRequest) (*echov1.CallResponse, error) {
	return &echov1.CallResponse{Message: "echo: " + req.GetMessage(), From: "test-f", Phase: "target"}, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	sdk.Serve(&testFModule{manifest: mf.ToProto()}, sdk.FunctionProvider{
		Name:     "test.e/v1",
		Register: func(s *grpc.Server) { echov1.RegisterEchoServer(s, echoServer{}) },
	})
}
