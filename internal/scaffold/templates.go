// SPDX-License-Identifier: Apache-2.0

package scaffold

import "text/template"

// Pas de "require genesis/sdk" explicite ici : sous go.work, un require
// versionné vers un module du monorepo (sans domaine réel) pousse
// `go mod tidy`/`go build` à tenter une résolution réseau plutôt que d'utiliser
// la substitution d'espace de travail. go build résout l'import correctement
// sans aucune ligne require (vérifié empiriquement) ; genesis/sdk reste la
// seule dépendance du module (docs/03-contrat-module.md).
var goModTemplate = template.Must(template.New("go.mod").Parse(
	`module {{.ModulePath}}

go 1.27.1
`))

var moduleYAMLTemplate = template.Must(template.New("module.yaml").Parse(
	`apiVersion: genesis/module/v1
name: {{.Name}}
version: 0.1.0
description: "TODO: décrire {{.Name}}"
layer: foundation
core: ">=0.1.0 <0.2.0"
protocol: 1

capabilities: []

provides:
{{- range .Provides}}
  - function: {{.}}
    phases: [target]
{{- end}}

requires: {}

config_schema: schema.json

secrets: []

resources: []

defaults: {}
`))

var schemaJSONTemplate = template.Must(template.New("schema.json").Parse(
	`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": true
}
`))

var mainGoTemplate = template.Must(template.New("main.go").Parse(
	`// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	_ "embed"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "genesis/sdk/go"
	modulev1 "genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

// {{.Name}} implémente modulev1.ModuleServer. Chaque étape est un stub tant
// qu'elle n'a pas été écrite (docs/10-ajouter-un-module.md, étape 3).
type {{.Name}}Module struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest
}

func (m *{{.Name}}Module) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *{{.Name}}Module) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func notImplemented(step string) error {
	return status.Errorf(codes.Unimplemented, "{{.Name}} : étape %s pas encore implémentée", step)
}

func (m *{{.Name}}Module) Check(context.Context, *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	return nil, notImplemented("Check")
}

func (m *{{.Name}}Module) SeedUp(context.Context, *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return nil, notImplemented("SeedUp")
}

func (m *{{.Name}}Module) SeedDown(context.Context, *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return nil, notImplemented("SeedDown")
}

func (m *{{.Name}}Module) Provision(context.Context, *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return nil, notImplemented("Provision")
}

func (m *{{.Name}}Module) Configure(context.Context, *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return nil, notImplemented("Configure")
}

func (m *{{.Name}}Module) Verify(context.Context, *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return nil, notImplemented("Verify")
}

func (m *{{.Name}}Module) Handover(context.Context, *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return nil, notImplemented("Handover")
}

func (m *{{.Name}}Module) Repoint(context.Context, *modulev1.RepointRequest) (*modulev1.StepResult, error) {
	return nil, notImplemented("Repoint")
}

func (m *{{.Name}}Module) Destroy(context.Context, *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return nil, notImplemented("Destroy")
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	sdk.Serve(&{{.Name}}Module{manifest: mf.ToProto()})
}
`))

var conformanceTestTemplate = template.Must(template.New("conformance_test.go").Parse(
	`// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	sdk "genesis/sdk/go"
	"genesis/sdk/go/moduletest"
)

// TestConformance : suite de conformité du SDK (docs/10-ajouter-un-module.md,
// étape 5). À lancer via 'go test ./... -run Conformance'.
func TestConformance(t *testing.T) {
	mf, err := sdk.LoadManifest("module.yaml")
	if err != nil {
		t.Fatalf("chargement de module.yaml : %v", err)
	}
	impl := &{{.Name}}Module{manifest: mf.ToProto()}
	moduletest.RunConformance(t, impl, "module.yaml")
}
`))
