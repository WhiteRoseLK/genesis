// SPDX-License-Identifier: Apache-2.0

//go:build docker

package runner_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/WhiteRoseLK/genesis/internal/runner"
	"github.com/WhiteRoseLK/genesis/internal/testutil"
)

func TestRunBlockingReturnsOutputAndExitCode(t *testing.T) {
	rt := testutil.RequireRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := rt.Run(ctx, runner.RunOptions{
		Image:   "alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6",
		Command: []string{"echo", "bonjour"},
	})
	if err != nil {
		t.Fatalf("Run : %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, attendu 0 : stderr=%s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "bonjour") {
		t.Errorf("Stdout = %q, attendu qu'il contienne bonjour", result.Stdout)
	}
}

func TestRunBlockingCapturesNonZeroExitCode(t *testing.T) {
	rt := testutil.RequireRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := rt.Run(ctx, runner.RunOptions{
		Image:   "alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6",
		Command: []string{"sh", "-c", "exit 7"},
	})
	if err != nil {
		t.Fatalf("Run : %v", err)
	}
	if result.ExitCode != 7 {
		t.Errorf("ExitCode = %d, attendu 7", result.ExitCode)
	}
}

func TestRunDetachedStopAndStatus(t *testing.T) {
	rt := testutil.RequireRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := rt.Run(ctx, runner.RunOptions{
		Image:   "alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6",
		Command: []string{"sleep", "60"},
		Detach:  true,
	})
	if err != nil {
		t.Fatalf("Run (détaché) : %v", err)
	}
	if result.ContainerID == "" {
		t.Fatal("ContainerID vide pour un conteneur détaché")
	}
	t.Cleanup(func() { _ = rt.Stop(context.Background(), result.ContainerID) })

	state, _, err := rt.Status(ctx, result.ContainerID)
	if err != nil {
		t.Fatalf("Status : %v", err)
	}
	if state != "running" {
		t.Errorf("state = %q, attendu running", state)
	}

	if err := rt.Stop(ctx, result.ContainerID); err != nil {
		t.Fatalf("Stop : %v", err)
	}
	// Stop supprime le conteneur (pas juste un arrêt réversible) : Status
	// dessus ensuite doit échouer, il n'existe plus.
	if _, _, err := rt.Status(ctx, result.ContainerID); err == nil {
		t.Error("Status après Stop : succès inattendu, le conteneur devrait être supprimé")
	}
}

func TestRunMountsHostDirectory(t *testing.T) {
	rt := testutil.RequireRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dir := t.TempDir()
	result, err := rt.Run(ctx, runner.RunOptions{
		Image:   "alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6",
		Command: []string{"sh", "-c", "echo contenu > /work/fichier.txt"},
		Mounts:  []runner.Mount{{HostPath: dir, ContainerPath: "/work"}},
	})
	if err != nil {
		t.Fatalf("Run : %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d : stderr=%s", result.ExitCode, result.Stderr)
	}

	data, err := os.ReadFile(dir + "/fichier.txt")
	if err != nil {
		t.Fatalf("lecture du fichier monté : %v", err)
	}
	if strings.TrimSpace(string(data)) != "contenu" {
		t.Errorf("contenu du fichier = %q, attendu contenu", data)
	}
}
