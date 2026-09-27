// SPDX-License-Identifier: Apache-2.0

// Test module for internal/modulehost: Check panics on purpose, to check that
// the core produces a clean error without stopping (M3 acceptance criterion,
// docs/08-milestones.md).
package main

import (
	"context"
	_ "embed"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

type panickingModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest
}

func (m *panickingModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *panickingModule) Check(context.Context, *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	panic("panicking: intentional panic for the supervision test")
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	sdk.Serve(&panickingModule{manifest: mf.ToProto()})
}
