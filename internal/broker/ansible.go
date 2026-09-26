// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
	target := req.GetTarget()
	files := map[string][]byte{
		"playbook.yml": req.GetPlaybookYaml(),
		"id_target":    []byte(target.GetSshPrivateKey()),
		"inventory.ini": []byte(fmt.Sprintf(
			"target ansible_host=%s ansible_port=%d ansible_user=%s ansible_ssh_private_key_file=/work/id_target ansible_ssh_common_args='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'\n",
			target.GetHost(), target.GetPort(), target.GetUser(),
		)),
	}

	playbookCmd := "ansible-playbook -i /work/inventory.ini /work/playbook.yml"
	var sensitive []string
	if vars := req.GetVars(); vars != nil {
		varsJSON, err := json.Marshal(vars.AsMap())
		if err != nil {
			return nil, fmt.Errorf("encodage des extra-vars : %w", err)
		}
		files["vars.json"] = varsJSON
		playbookCmd += " --extra-vars @/work/vars.json"
		sensitive = collectStrings(vars.AsMap())
	}
	sensitive = append(sensitive, target.GetSshPrivateKey())

	// Les fichiers (clé privée, extra-vars contenant des secrets) ne
	// touchent jamais le disque de la graine : ils transitent par l'entrée
	// standard, sous forme d'archive tar, vers un tmpfs du conteneur. Aucun
	// autre utilisateur de la graine ne peut les lire, quel que soit l'UID
	// effectif du conteneur.
	archive, err := tarFiles(files)
	if err != nil {
		return nil, fmt.Errorf("préparation des fichiers du playbook : %w", err)
	}

	result, err := a.runtime.Run(ctx, runner.RunOptions{
		Image:   ansibleImage,
		Command: []string{"sh", "-c", "tar -xf - -C /work && exec " + playbookCmd},
		// 1777 : l'image n'exécute pas ansible en root ; le tmpfs est privé
		// au conteneur et les fichiers y sont extraits en 0600.
		Tmpfs: []string{"/work:rw,mode=1777"},
		Stdin: archive,
		Env:   map[string]string{"ANSIBLE_HOST_KEY_CHECKING": "False"},
	})
	if err != nil {
		return nil, fmt.Errorf("exécution du conteneur ansible : %w", err)
	}

	return &ansiblev1.RunPlaybookResponse{
		Ok:     result.ExitCode == 0,
		Output: redactValues(result.Stdout+result.Stderr, sensitive),
	}, nil
}

// tarFiles construit une archive tar en mémoire, chaque fichier en 0600.
func tarFiles(files map[string][]byte) ([]byte, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range names {
		content := files[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content))}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(content); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// minRedactLen évite de masquer des valeurs courtes et banales (ports,
// booléens, noms d'utilisateur) qui rendraient la sortie illisible.
const minRedactLen = 8

// collectStrings renvoie toutes les chaînes contenues dans v, récursivement.
func collectStrings(v any) []string {
	var out []string
	switch t := v.(type) {
	case string:
		out = append(out, t)
	case map[string]any:
		for _, e := range t {
			out = append(out, collectStrings(e)...)
		}
	case []any:
		for _, e := range t {
			out = append(out, collectStrings(e)...)
		}
	}
	return out
}

// redactValues masque dans out chaque valeur de values (et chacune de ses
// lignes, Ansible pouvant réafficher un bloc PEM ligne par ligne). Les
// extra-vars transportent des secrets (certificats, role_id/secret_id) :
// la sortie du playbook remonte jusqu'aux erreurs et journaux du moteur et
// ne doit jamais les contenir (règle « aucun secret en clair »).
func redactValues(out string, values []string) string {
	var needles []string
	for _, v := range values {
		for _, line := range strings.Split(v, "\n") {
			if line = strings.TrimSpace(line); len(line) >= minRedactLen {
				needles = append(needles, line)
			}
		}
	}
	// Les plus longues d'abord : une valeur contenue dans une autre ne doit
	// pas empêcher de masquer la plus longue.
	sort.Slice(needles, func(i, j int) bool { return len(needles[i]) > len(needles[j]) })
	for _, n := range needles {
		out = strings.ReplaceAll(out, n, "***")
	}
	return out
}
