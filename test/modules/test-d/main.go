// SPDX-License-Identifier: Apache-2.0

// test-d requiert test.a/v1 et fournit test.d/v1 : preuve d'extensibilité du
// jalon J4 (docs/08-jalons.md) — ajouté après coup, sans toucher au
// résolveur, au planificateur, au moteur ni aux autres modules.
package main

import (
	"context"
	_ "embed"
	"fmt"

	"google.golang.org/grpc"

	sdk "genesis/sdk/go"
	echov1 "genesis/sdk/go/gen/functions/test/echo/v1"
	modulev1 "genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

type testDModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest
	broker   *sdk.BrokerClient
}

func (m *testDModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *testDModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *testDModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *testDModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	flags := sdk.StateMap(req.GetState())
	if boolFlag(flags, "verified") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_CONFORME}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_A_FAIRE}, nil
}

func (m *testDModule) Provision(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "provisioned")
}

func (m *testDModule) Configure(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "configured")
}

func (m *testDModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	conn, err := m.broker.Dial(req.GetBrokerToken())
	if err != nil {
		return nil, fmt.Errorf("connexion au broker : %w", err)
	}
	resp, err := echov1.NewEchoClient(conn).Call(ctx, &echov1.CallRequest{Message: "salut depuis test-d"})
	if err != nil {
		return nil, fmt.Errorf("appel de test.a/v1 via le broker : %w", err)
	}
	if resp.GetFrom() != "test-a" {
		return nil, fmt.Errorf("réponse inattendue de test.a/v1 : %+v", resp)
	}
	return setFlag(req, "verified")
}

func (m *testDModule) Destroy(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
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

// echoServer implémente functions/test/echo/v1 pour la fonction test.d/v1.
type echoServer struct {
	echov1.UnimplementedEchoServer
}

func (echoServer) Call(_ context.Context, req *echov1.CallRequest) (*echov1.CallResponse, error) {
	return &echov1.CallResponse{Message: "echo: " + req.GetMessage(), From: "test-d", Phase: "target"}, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	sdk.Serve(&testDModule{manifest: mf.ToProto()}, sdk.FunctionProvider{
		Name:     "test.d/v1",
		Register: func(s *grpc.Server) { echov1.RegisterEchoServer(s, echoServer{}) },
	})
}
