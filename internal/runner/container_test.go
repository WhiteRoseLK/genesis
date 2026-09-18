// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func requireRuntime(t *testing.T) *ContainerRuntime {
	t.Helper()
	rt, err := DetectContainerRuntime("auto")
	if err != nil {
		t.Skipf("aucun runtime de conteneur disponible : %v", err)
	}
	return rt
}

func TestRunBlockingReturnsOutputAndExitCode(t *testing.T) {
	rt := requireRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := rt.Run(ctx, RunOptions{
		Image:   "alpine:3",
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
	rt := requireRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := rt.Run(ctx, RunOptions{
		Image:   "alpine:3",
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
	rt := requireRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := rt.Run(ctx, RunOptions{
		Image:   "alpine:3",
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
	state, _, err = rt.Status(ctx, result.ContainerID)
	if err != nil {
		t.Fatalf("Status après Stop : %v", err)
	}
	if state == "running" {
		t.Errorf("state après Stop = %q, ne devrait plus être running", state)
	}
}

func TestRunMountsHostDirectory(t *testing.T) {
	rt := requireRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dir := t.TempDir()
	result, err := rt.Run(ctx, RunOptions{
		Image:   "alpine:3",
		Command: []string{"sh", "-c", "echo contenu > /work/fichier.txt"},
		Mounts:  []Mount{{HostPath: dir, ContainerPath: "/work"}},
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
