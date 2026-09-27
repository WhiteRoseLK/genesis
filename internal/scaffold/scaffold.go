// SPDX-License-Identifier: Apache-2.0

// Package scaffold generates the skeleton of a new module
// (docs/10-adding-a-module.md, step 1: `genesis modules scaffold`).
package scaffold

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

// Generate creates modules/<name>/ under repoRoot (module.yaml, main.go,
// schema.json, conformance test, go.mod) for the provides functions (e.g.
// ["dns.zone/v1", "dns.resolver/v1"]), then adds it to the Go workspace (`go
// work use`) and resolves its dependencies (`go mod tidy`) — without any file
// outside modules/<name>/ (apart from go.work) having to be edited by hand
// (docs/02-architecture.md: "adding a module must not require any change
// outside its directory").
func Generate(repoRoot, name string, provides []string) (dir string, err error) {
	if name == "" {
		return "", fmt.Errorf("the module name cannot be empty")
	}
	if len(provides) == 0 {
		return "", fmt.Errorf("--provides must list at least one function (e.g. dns.zone/v1)")
	}

	dir = filepath.Join(repoRoot, "modules", name)
	if _, statErr := os.Stat(dir); statErr == nil {
		return "", fmt.Errorf("%s already exists", dir)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil { //nolint:gosec // G301: repository sources, not sensitive data
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}

	data := templateData{
		Name:       name,
		ModulePath: RepoModulePath + "/modules/" + name,
		SDKPath:    RepoModulePath + "/sdk",
		Provides:   provides,
	}

	files := map[string]*template.Template{
		"go.mod":              goModTemplate,
		"module.yaml":         moduleYAMLTemplate,
		"main.go":             mainGoTemplate,
		"schema.json":         schemaJSONTemplate,
		"conformance_test.go": conformanceTestTemplate,
	}
	for filename, tmpl := range files {
		if err := renderFile(filepath.Join(dir, filename), tmpl, data); err != nil {
			return "", err
		}
	}

	if err := runIn(repoRoot, "go", "work", "use", filepath.Join(".", "modules", name)); err != nil {
		return "", fmt.Errorf("adding to the Go workspace (go work use): %w", err)
	}
	// Outside the workspace: the generated go.mod must be self-contained (the
	// skeleton's third-party dependencies and go.sum), as checked in CI.
	if err := runIn(dir, "go", "mod", "tidy"); err != nil {
		return "", fmt.Errorf("resolving dependencies (go mod tidy): %w", err)
	}
	if err := runIn(dir, "go", "build", "./..."); err != nil {
		return "", fmt.Errorf("the generated module does not build: %w", err)
	}

	return dir, nil
}

// RepoModulePath is the repository's Go module path.
const RepoModulePath = "github.com/WhiteRoseLK/genesis"

type templateData struct {
	Name       string
	ModulePath string
	SDKPath    string
	Provides   []string
}

func renderFile(path string, tmpl *template.Template, data templateData) error {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("generating %s: %w", path, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil { //nolint:gosec // G306: repository source file
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func runIn(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...) //nolint:noctx // short command run by the CLI, no call context
	cmd.Dir = dir
	if len(args) > 0 && args[0] == "mod" {
		cmd.Env = append(os.Environ(), "GOWORK=off")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s (in %s): %w\n%s", name, args, dir, err, out)
	}
	return nil
}
