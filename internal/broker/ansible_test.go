// SPDX-License-Identifier: Apache-2.0

//go:build docker

package broker

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os/exec"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/WhiteRoseLK/genesis/internal/runner"
	"github.com/WhiteRoseLK/genesis/internal/testutil"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
)

// sshTarget is a disposable SSH container acting as the target VM, to really
// test core.ansible/v1 without a hypervisor (docs/08-milestones.md, M5).
//
// Environment note: this test checks the result through `docker exec` rather
// than dialling the target container's IP directly (the docker0 bridge network
// is not reachable from the test process in this environment, only from
// another container or through the docker daemon) — what RunPlaybook really
// does (ansible container -> target container) is not affected, only the
// external check is.
type sshTarget struct {
	containerID   string
	ip            string
	port          int32
	user          string
	privateKeyPEM string
}

func startSSHTarget(t *testing.T, rt *runner.ContainerRuntime) *sshTarget {
	t.Helper()
	ctx := context.Background()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := gossh.NewPublicKey(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}

	result, err := rt.Run(ctx, runner.RunOptions{
		Image: "lscr.io/linuxserver/openssh-server:10.3_p1-r1-ls237@sha256:946fa26105e0ec212fdf821b9ddc59aab65f2c2d07c02b25ff0f5001fc332ff0",
		Env: map[string]string{
			"PUBLIC_KEY":      string(gossh.MarshalAuthorizedKey(sshPub)),
			"USER_NAME":       "genesis",
			"PASSWORD_ACCESS": "false",
			"SUDO_ACCESS":     "true",
		},
		Detach: true,
	})
	if err != nil {
		t.Fatalf("starting the SSH target container: %v", err)
	}
	t.Cleanup(func() { _ = rt.Stop(context.Background(), result.ContainerID) })

	ip := waitForIP(t, rt, result.ContainerID)

	return &sshTarget{
		containerID:   result.ContainerID,
		ip:            ip,
		port:          2222,
		user:          "genesis",
		privateKeyPEM: string(pem.EncodeToMemory(block)),
	}
}

func waitForIP(t *testing.T, rt *runner.ContainerRuntime, containerID string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ip, err := rt.InspectIP(context.Background(), containerID); err == nil {
			return ip
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("container %s never got an IP address", containerID)
	return ""
}

// dockerExec checks what really happened on the target by inspecting its file
// system through the docker daemon (docker exec), independently of anything
// ansible reported.
func dockerExec(t *testing.T, containerID string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"exec", containerID}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("docker exec %v: %v\n%s", args, err, stderr.String())
	}
	return stdout.String()
}

// runPlaybookWithRetry retries RunPlaybook: sshd in the target container takes
// a few seconds to generate its host keys and become reachable, and nothing in
// this test can wait for it directly (see the note on sshTarget).
func runPlaybookWithRetry(t *testing.T, server *ansibleServer, req *ansiblev1.RunPlaybookRequest) *ansiblev1.RunPlaybookResponse {
	t.Helper()
	var lastResp *ansiblev1.RunPlaybookResponse
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		resp, err := server.RunPlaybook(context.Background(), req)
		if err == nil && resp.GetOk() {
			return resp
		}
		lastResp, lastErr = resp, err
		time.Sleep(5 * time.Second)
	}
	if lastErr != nil {
		t.Fatalf("RunPlaybook failed after several attempts: %v", lastErr)
	}
	t.Fatalf("RunPlaybook never succeeded:\n%s", lastResp.GetOutput())
	return nil
}

// TestRunPlaybookActuallyConfiguresTarget proves the complete core.ansible/v1
// mechanism (docs/08-milestones.md, M5): the ansible container really connects
// to the target over SSH and really runs the task there — checked with `docker
// exec` on the target, not by taking ansible's word for it.
func TestRunPlaybookActuallyConfiguresTarget(t *testing.T) {
	rt := testutil.RequireRuntime(t)
	target := startSSHTarget(t, rt)

	// ansible.builtin.raw rather than copy/template: this test's disposable
	// SSH image has no Python (the real target VMs, a Debian cloud image, have
	// one — docs/01-vision-scope.md), and raw does not need it.
	const playbook = `---
- hosts: target
  gather_facts: false
  tasks:
    - name: write a marker
      ansible.builtin.raw: echo -n "ok" > /tmp/marker.txt
`
	server := &ansibleServer{runtime: rt}
	resp := runPlaybookWithRetry(t, server, &ansiblev1.RunPlaybookRequest{
		Target: &ansiblev1.Target{
			Host:          target.ip,
			Port:          target.port,
			User:          target.user,
			SshPrivateKey: target.privateKeyPEM,
		},
		PlaybookYaml: []byte(playbook),
	})
	if !resp.GetOk() {
		t.Fatalf("RunPlaybook failed:\n%s", resp.GetOutput())
	}

	got := dockerExec(t, target.containerID, "cat", "/tmp/marker.txt")
	if got != "ok" {
		t.Errorf("content of /tmp/marker.txt = %q, want %q — the playbook did not really act on the target", got, "ok")
	}
}
