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
		Command: []string{"echo", "hello"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0: stderr=%s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("Stdout = %q, want it to contain hello", result.Stdout)
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
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", result.ExitCode)
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
		t.Fatalf("Run (detached): %v", err)
	}
	if result.ContainerID == "" {
		t.Fatal("empty ContainerID for a detached container")
	}
	t.Cleanup(func() { _ = rt.Stop(context.Background(), result.ContainerID) })

	state, _, err := rt.Status(ctx, result.ContainerID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state != "running" {
		t.Errorf("state = %q, want running", state)
	}

	if err := rt.Stop(ctx, result.ContainerID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Stop removes the container (not just a reversible stop): a Status on it
	// afterwards must fail, it no longer exists.
	if _, _, err := rt.Status(ctx, result.ContainerID); err == nil {
		t.Error("Status after Stop: unexpected success, the container should have been removed")
	}
}

func TestRunMountsHostDirectory(t *testing.T) {
	rt := testutil.RequireRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dir := t.TempDir()
	result, err := rt.Run(ctx, runner.RunOptions{
		Image:   "alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6",
		Command: []string{"sh", "-c", "echo content > /work/file.txt"},
		Mounts:  []runner.Mount{{HostPath: dir, ContainerPath: "/work"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d: stderr=%s", result.ExitCode, result.Stderr)
	}

	data, err := os.ReadFile(dir + "/file.txt")
	if err != nil {
		t.Fatalf("reading the mounted file: %v", err)
	}
	if strings.TrimSpace(string(data)) != "content" {
		t.Errorf("file content = %q, want content", data)
	}
}

func TestRunWithFilesExchangesThroughContainerLayer(t *testing.T) {
	rt := testutil.RequireRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := rt.Run(ctx, runner.RunOptions{
		Image:   "alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6",
		Command: []string{"sh", "-c", "tr a-z A-Z < /work/in.txt > /work/out.txt && echo done"},
		Files:   map[string][]byte{"/work/in.txt": []byte("secret")},
		Collect: []string{"/work/out.txt"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "done") {
		t.Errorf("Stdout = %q, want \"done\"", result.Stdout)
	}
	if got := string(result.Collected["/work/out.txt"]); got != "SECRET" {
		t.Errorf("collected file = %q, want SECRET", got)
	}
}
