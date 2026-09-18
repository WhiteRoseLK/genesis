// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/grpc"

	"genesis/internal/runner"
	ansiblev1 "genesis/sdk/go/gen/functions/core/ansible/v1"
)

// ansibleImage exécute les playbooks sans rien installer sur la graine
// (ADR-002). TODO(J6+) : rendre configurable (image épinglée par digest,
// miroir privé pour le profil air-gap de l'ADR-008).
const ansibleImage = "willhallonline/ansible:2.16-alpine-3.19"

// NativeAnsible construit le fournisseur core.ansible/v1, natif au cœur.
func NativeAnsible(rt *runner.ContainerRuntime) nativeFactory {
	return func(_ string) func(*grpc.Server) {
		return func(s *grpc.Server) {
			ansiblev1.RegisterAnsibleServer(s, &ansibleServer{runtime: rt})
		}
	}
}

type ansibleServer struct {
	ansiblev1.UnimplementedAnsibleServer
	runtime *runner.ContainerRuntime
}

func (a *ansibleServer) RunPlaybook(ctx context.Context, req *ansiblev1.RunPlaybookRequest) (*ansiblev1.RunPlaybookResponse, error) {
	dir, err := os.MkdirTemp("", "genesis-ansible-*")
	if err != nil {
		return nil, fmt.Errorf("préparation du répertoire de travail : %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	// MkdirTemp crée en 0700 : le conteneur ansible tourne sous son propre
	// UID interne (généralement distinct du nôtre, parfois par un décalage
	// d'espace de noms utilisateur), qui ne pourrait pas lire un répertoire
	// 0700 appartenant à un autre UID. Lisible par tous plutôt que de
	// deviner l'UID effectif du conteneur.
	if err := os.Chmod(dir, 0o755); err != nil {
		return nil, fmt.Errorf("permissions du répertoire de travail : %w", err)
	}

	target := req.GetTarget()

	if err := os.WriteFile(filepath.Join(dir, "playbook.yml"), req.GetPlaybookYaml(), 0o644); err != nil {
		return nil, fmt.Errorf("écriture du playbook : %w", err)
	}

	keyPath := filepath.Join(dir, "id_target")
	if err := os.WriteFile(keyPath, []byte(target.GetSshPrivateKey()), 0o644); err != nil {
		return nil, fmt.Errorf("écriture de la clé privée : %w", err)
	}

	inventory := fmt.Sprintf(
		"target ansible_host=%s ansible_port=%d ansible_user=%s ansible_ssh_private_key_file=/work/id_target ansible_ssh_common_args='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'\n",
		target.GetHost(), target.GetPort(), target.GetUser(),
	)
	if err := os.WriteFile(filepath.Join(dir, "inventory.ini"), []byte(inventory), 0o644); err != nil {
		return nil, fmt.Errorf("écriture de l'inventaire : %w", err)
	}

	command := []string{"ansible-playbook", "-i", "/work/inventory.ini", "/work/playbook.yml"}
	if vars := req.GetVars(); vars != nil {
		varsJSON, err := json.Marshal(vars.AsMap())
		if err != nil {
			return nil, fmt.Errorf("encodage des extra-vars : %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "vars.json"), varsJSON, 0o644); err != nil {
			return nil, fmt.Errorf("écriture des extra-vars : %w", err)
		}
		command = append(command, "--extra-vars", "@/work/vars.json")
	}

	result, err := a.runtime.Run(ctx, runner.RunOptions{
		Image:   ansibleImage,
		Command: command,
		Mounts:  []runner.Mount{{HostPath: dir, ContainerPath: "/work", ReadOnly: false}},
		Env:     map[string]string{"ANSIBLE_HOST_KEY_CHECKING": "False"},
	})
	if err != nil {
		return nil, fmt.Errorf("exécution du conteneur ansible : %w", err)
	}

	return &ansiblev1.RunPlaybookResponse{
		Ok:     result.ExitCode == 0,
		Output: result.Stdout + result.Stderr,
	}, nil
}
