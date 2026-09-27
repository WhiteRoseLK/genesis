// SPDX-License-Identifier: Apache-2.0

// test-kv provides secrets.kv/v1 in the target phase (docs/08-milestones.md,
// M7): in-memory storage, to prove the file->vault migration mechanism in
// internal/engine without depending on the real Vault product.
package main

import (
	"context"
	_ "embed"
	"sync"

	"google.golang.org/grpc"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	secretskvv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/secrets/kv/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

type testKVModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest
}

func (m *testKVModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *testKVModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *testKVModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	flags := sdk.StateMap(req.GetState())
	if boolFlag(flags, "verified") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_TODO}, nil
}

func (m *testKVModule) Provision(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "provisioned")
}

func (m *testKVModule) Configure(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "configured")
}

func (m *testKVModule) Verify(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return setFlag(req, "verified")
}

func (m *testKVModule) Destroy(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
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

type kvServer struct {
	secretskvv1.UnimplementedSecretsKVServer
	mu     sync.Mutex
	values map[string]string
}

func (s *kvServer) Read(_ context.Context, req *secretskvv1.ReadRequest) (*secretskvv1.ReadResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[req.GetRef()]
	return &secretskvv1.ReadResponse{Value: v, Found: ok}, nil
}

func (s *kvServer) Write(_ context.Context, req *secretskvv1.WriteRequest) (*secretskvv1.WriteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[req.GetRef()] = req.GetValue()
	return &secretskvv1.WriteResponse{}, nil
}

func (s *kvServer) List(_ context.Context, req *secretskvv1.ListRequest) (*secretskvv1.ListResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var refs []string
	for ref := range s.values {
		refs = append(refs, ref)
	}
	return &secretskvv1.ListResponse{Refs: refs}, nil
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	sdk.Serve(&testKVModule{manifest: mf.ToProto()}, sdk.FunctionProvider{
		Name:     "secrets.kv/v1",
		Register: func(s *grpc.Server) { secretskvv1.RegisterSecretsKVServer(s, &kvServer{}) },
	})
}
