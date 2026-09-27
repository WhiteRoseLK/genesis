// SPDX-License-Identifier: Apache-2.0

// Package scaffold génère le squelette d'un nouveau module
// (docs/10-adding-a-module.md, étape 1 : `genesis modules scaffold`).
package scaffold

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

// Generate crée modules/<name>/ sous repoRoot (module.yaml, main.go,
// schema.json, test de conformité, go.mod) pour les fonctions provides
// (ex. ["dns.zone/v1", "dns.resolver/v1"]), puis l'ajoute au workspace Go
// (`go work use`) et résout ses dépendances (`go mod tidy`) — sans qu'aucun
// fichier hors modules/<name>/ (à part go.work) n'ait à être édité à la main
// (docs/02-architecture.md : "ajouter un module ne doit nécessiter aucune
// modification hors de son répertoire").
func Generate(repoRoot, name string, provides []string) (dir string, err error) {
	if name == "" {
		return "", fmt.Errorf("le nom du module ne peut pas être vide")
	}
	if len(provides) == 0 {
		return "", fmt.Errorf("--provides doit lister au moins une fonction (ex. dns.zone/v1)")
	}

	dir = filepath.Join(repoRoot, "modules", name)
	if _, statErr := os.Stat(dir); statErr == nil {
		return "", fmt.Errorf("%s existe déjà", dir)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil { //nolint:gosec // G301 : sources du dépôt, pas des données sensibles
		return "", fmt.Errorf("création de %s : %w", dir, err)
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
		return "", fmt.Errorf("ajout au workspace Go (go work use) : %w", err)
	}
	// Hors espace de travail : le go.mod généré doit se suffire à lui-même
	// (dépendances tierces du squelette et go.sum), comme vérifié en CI.
	if err := runIn(dir, "go", "mod", "tidy"); err != nil {
		return "", fmt.Errorf("résolution des dépendances (go mod tidy) : %w", err)
	}
	if err := runIn(dir, "go", "build", "./..."); err != nil {
		return "", fmt.Errorf("le module généré ne compile pas : %w", err)
	}

	return dir, nil
}

// RepoModulePath est le chemin de module Go du dépôt.
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
		return fmt.Errorf("génération de %s : %w", path, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil { //nolint:gosec // G306 : fichier source du dépôt
		return fmt.Errorf("écriture de %s : %w", path, err)
	}
	return nil
}

func runIn(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...) //nolint:noctx // commande courte lancée par la CLI, sans contexte d'appel
	cmd.Dir = dir
	if len(args) > 0 && args[0] == "mod" {
		cmd.Env = append(os.Environ(), "GOWORK=off")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s (dans %s) : %w\n%s", name, args, dir, err, out)
	}
	return nil
}
