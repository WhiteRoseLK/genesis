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

// sshTarget est un conteneur SSH jetable faisant office de VM cible, pour
// tester réellement core.ansible/v1 sans hyperviseur (docs/08-milestones.md, J5).
//
// Remarque d'environnement : ce test vérifie le résultat via `docker exec`
// plutôt qu'en dialant l'IP du conteneur cible directement (le réseau pont
// docker0 n'est pas joignable depuis le process de test dans cet
// environnement, seulement depuis un autre conteneur ou via le daemon
// docker) — ce que fait réellement RunPlaybook (conteneur ansible ->
// conteneur cible) n'est pas affecté, seule la vérification externe l'est.
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
		t.Fatalf("démarrage du conteneur cible SSH : %v", err)
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
	t.Fatalf("le conteneur %s n'a jamais eu d'adresse IP", containerID)
	return ""
}

// dockerExec vérifie ce qui s'est réellement passé sur la cible en
// inspectant son système de fichiers via le daemon docker (docker exec),
// indépendamment de tout ce qu'ansible a rapporté.
func dockerExec(t *testing.T, containerID string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"exec", containerID}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("docker exec %v : %v\n%s", args, err, stderr.String())
	}
	return stdout.String()
}

// runPlaybookWithRetry retente RunPlaybook : sshd dans le conteneur cible met
// quelques secondes à générer ses clés d'hôte et devenir joignable, et rien
// dans ce test ne peut l'attendre directement (voir la note sur sshTarget).
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
		t.Fatalf("RunPlaybook a échoué après plusieurs tentatives : %v", lastErr)
	}
	t.Fatalf("RunPlaybook n'a jamais réussi :\n%s", lastResp.GetOutput())
	return nil
}

// TestRunPlaybookActuallyConfiguresTarget prouve le mécanisme complet de
// core.ansible/v1 (docs/08-milestones.md, J5) : le conteneur ansible se connecte
// réellement en SSH à la cible et y exécute réellement la tâche — vérifié via
// `docker exec` sur la cible, pas en croyant la réponse d'ansible sur parole.
func TestRunPlaybookActuallyConfiguresTarget(t *testing.T) {
	rt := testutil.RequireRuntime(t)
	target := startSSHTarget(t, rt)

	// ansible.builtin.raw plutôt que copy/template : l'image SSH jetable de
	// ce test n'a pas Python (les vraies VM cibles, image cloud Debian, en
	// ont un — docs/01-vision-scope.md), et raw n'en a pas besoin.
	const playbook = `---
- hosts: target
  gather_facts: false
  tasks:
    - name: écrire un marqueur
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
		t.Fatalf("RunPlaybook a échoué :\n%s", resp.GetOutput())
	}

	got := dockerExec(t, target.containerID, "cat", "/tmp/marker.txt")
	if got != "ok" {
		t.Errorf("contenu de /tmp/marker.txt = %q, attendu %q — le playbook n'a pas réellement agi sur la cible", got, "ok")
	}
}
