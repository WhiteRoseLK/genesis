// SPDX-License-Identifier: Apache-2.0

// Module de test pour internal/modulehost : Check panique volontairement,
// pour vérifier que le cœur produit une erreur propre sans s'arrêter
// (critère d'acceptation du jalon J3, docs/08-milestones.md).
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
	panic("panicking : panique intentionnelle pour le test de supervision")
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	sdk.Serve(&panickingModule{manifest: mf.ToProto()})
}
