// SPDX-License-Identifier: Apache-2.0

// test-c requiert test.b/v1 et fournit test.c/v1 (docs/08-jalons.md, J4) :
// ferme la chaîne test-a -> test-b -> test-c.
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

type testCModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest
	broker   *sdk.BrokerClient
}

func (m *testCModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *testCModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *testCModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *testCModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	flags := sdk.StateMap(req.GetState())
	if boolFlag(flags, "verified") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_CONFORME}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_A_FAIRE}, nil
}

func (m *testCModule) Provision(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "provisioned")
}

func (m *testCModule) Configure(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "configured")
}

func (m *testCModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	conn, err := m.broker.Dial(req.GetBrokerToken())
	if err != nil {
		return nil, fmt.Errorf("connexion au broker : %w", err)
	}
	resp, err := echov1.NewEchoClient(conn).Call(ctx, &echov1.CallRequest{Message: "salut depuis test-c"})
	if err != nil {
		return nil, fmt.Errorf("appel de test.b/v1 via le broker : %w", err)
	}
	if resp.GetFrom() != "test-b" {
		return nil, fmt.Errorf("réponse inattendue de test.b/v1 : %+v", resp)
	}
	return setFlag(req, "verified")
}

func (m *testCModule) Destroy(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
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

// echoServer implémente functions/test/echo/v1 pour la fonction test.c/v1.
type echoServer struct {
	echov1.UnimplementedEchoServer
}

func (echoServer) Call(_ context.Context, req *echov1.CallRequest) (*echov1.CallResponse, error) {
	return &echov1.CallResponse{Message: "echo: " + req.GetMessage(), From: "test-c", Phase: "target"}, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	sdk.Serve(&testCModule{manifest: mf.ToProto()}, sdk.FunctionProvider{
		Name:     "test.c/v1",
		Register: func(s *grpc.Server) { echov1.RegisterEchoServer(s, echoServer{}) },
	})
}
