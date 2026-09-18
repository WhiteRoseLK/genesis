// SPDX-License-Identifier: Apache-2.0

// Package runner pilote le runtime de conteneurs (docker ou podman) de la
// graine en ligne de commande — docs/02-architecture.md : "les runners sont
// exposés aux modules comme fonctions intégrées" (core.container/v1,
// core.ansible/v1). Orchestrer l'outil existant plutôt que réimplémenter un
// client de l'API Docker (ADR-002, même logique appliquée aux runners).
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ContainerRuntime pilote un binaire docker/podman déjà installé sur la
// graine (docs/04-spec.md : seed.container_runtime).
type ContainerRuntime struct {
	binary string
}

// DetectContainerRuntime cherche preferred ("docker", "podman") ou, si vide
// ou "auto", le premier des deux trouvé sur le PATH.
func DetectContainerRuntime(preferred string) (*ContainerRuntime, error) {
	candidates := []string{"docker", "podman"}
	if preferred != "" && preferred != "auto" {
		candidates = []string{preferred}
	}
	for _, bin := range candidates {
		if path, err := exec.LookPath(bin); err == nil {
			return &ContainerRuntime{binary: path}, nil
		}
	}
	return nil, fmt.Errorf("aucun runtime de conteneur trouvé parmi %v (installer docker ou podman)", candidates)
}

// Mount est un montage bind host -> conteneur.
type Mount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

// RunOptions décrit un conteneur à démarrer.
type RunOptions struct {
	Name    string
	Image   string
	Command []string
	Env     map[string]string
	Mounts  []Mount
	// Detach : voir docs/proto core.container/v1 RunRequest.
	Detach bool
}

// RunResult est le résultat de Run.
type RunResult struct {
	ContainerID string
	ExitCode    int
	Stdout      string
	Stderr      string
}

// Run démarre un conteneur. Non détaché (par défaut) : bloque jusqu'à sa fin
// et renvoie sa sortie. Détaché : rend la main immédiatement avec l'ID.
func (r *ContainerRuntime) Run(ctx context.Context, opts RunOptions) (*RunResult, error) {
	args := []string{"run"}
	if opts.Detach {
		args = append(args, "-d")
	} else {
		args = append(args, "--rm")
	}
	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}
	for k, v := range opts.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	for _, m := range opts.Mounts {
		spec := fmt.Sprintf("%s:%s", m.HostPath, m.ContainerPath)
		if m.ReadOnly {
			spec += ":ro"
		}
		args = append(args, "-v", spec)
	}
	args = append(args, opts.Image)
	args = append(args, opts.Command...)

	cmd := exec.CommandContext(ctx, r.binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	if opts.Detach {
		if runErr != nil {
			return nil, fmt.Errorf("%s run -d %s : %w\n%s", r.binary, opts.Image, runErr, stderr.String())
		}
		return &RunResult{ContainerID: strings.TrimSpace(stdout.String())}, nil
	}

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("%s run %s : %w\n%s", r.binary, opts.Image, runErr, stderr.String())
		}
	}
	return &RunResult{ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

// Stop arrête et supprime un conteneur détaché. "docker stop" seul ne
// libère pas le nom du conteneur (bug trouvé en construisant J6 :
// fake-compute.DeleteVM puis EnsureVM du même nom échouait avec "Conflict.
// The container name ... is already in use") — Stop doit vraiment vouloir
// dire "ce conteneur peut disparaître", pas juste "en pause" : rien
// n'appelle Status après Stop pour vouloir l'inspecter encore.
func (r *ContainerRuntime) Stop(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, r.binary, "rm", "-f", containerID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s rm -f %s : %w\n%s", r.binary, containerID, err, stderr.String())
	}
	return nil
}

// InspectIP retourne l'adresse IP du conteneur sur son réseau (le premier
// réseau trouvé — suffisant tant qu'un conteneur n'est attaché qu'à un seul
// réseau, ce qui est le cas de tout ce que le cœur démarre aujourd'hui).
func (r *ContainerRuntime) InspectIP(ctx context.Context, containerID string) (string, error) {
	cmd := exec.CommandContext(ctx, r.binary, "inspect", "-f",
		"{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", containerID)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s inspect %s : %w\n%s", r.binary, containerID, err, stderr.String())
	}
	ip := strings.TrimSpace(stdout.String())
	if ip == "" {
		return "", fmt.Errorf("aucune adresse IP trouvée pour %s", containerID)
	}
	return ip, nil
}

// Status inspecte l'état d'un conteneur.
func (r *ContainerRuntime) Status(ctx context.Context, containerID string) (state string, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, r.binary, "inspect", "--format", "{{.State.Status}} {{.State.ExitCode}}", containerID)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return "unknown", 0, fmt.Errorf("%s inspect %s : %w\n%s", r.binary, containerID, runErr, stderr.String())
	}
	fields := strings.Fields(strings.TrimSpace(stdout.String()))
	if len(fields) != 2 {
		return "unknown", 0, fmt.Errorf("sortie inattendue de %s inspect %s : %q", r.binary, containerID, stdout.String())
	}
	var code int
	if _, scanErr := fmt.Sscanf(fields[1], "%d", &code); scanErr != nil {
		return fields[0], 0, fmt.Errorf("code de sortie invalide dans %q : %w", stdout.String(), scanErr)
	}
	return fields[0], code, nil
}
