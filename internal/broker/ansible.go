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

	"github.com/WhiteRoseLK/genesis/internal/runner"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
)

// ansibleImage runs the playbooks without installing anything on the seed
// (ADR-002). Pinned by version and digest (reproducibility, supply chain).
// TODO: make it configurable (private mirror for the air-gapped profile of
// ADR-008).
const ansibleImage = "willhallonline/ansible:2.16-alpine-3.19@sha256:6f9d1ec5bdb30f0a06d7293e8cd9eb3a41ad4521c05b2a94ccf1604e0946b7b6"

// NativeAnsible builds the core.ansible/v1 provider, native to the core.
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
			return nil, fmt.Errorf("encoding the extra-vars: %w", err)
		}
		files["vars.json"] = varsJSON
		playbookCmd += " --extra-vars @/work/vars.json"
		sensitive = collectStrings(vars.AsMap())
	}
	sensitive = append(sensitive, target.GetSshPrivateKey())

	// The files (private key, extra-vars holding secrets) never touch the
	// seed's disk: they go through standard input, as a tar archive, into a
	// tmpfs of the container. No other user of the seed can read them,
	// whatever the container's effective UID.
	archive, err := tarFiles(files)
	if err != nil {
		return nil, fmt.Errorf("preparing the playbook files: %w", err)
	}

	result, err := a.runtime.Run(ctx, runner.RunOptions{
		Image:   ansibleImage,
		Command: []string{"sh", "-c", "tar -xf - -C /work && exec " + playbookCmd},
		// 1777: the image does not run ansible as root; the tmpfs is private
		// to the container and the files are extracted there as 0600.
		Tmpfs: []string{"/work:rw,mode=1777"},
		Stdin: archive,
		Env:   map[string]string{"ANSIBLE_HOST_KEY_CHECKING": "False"},
	})
	if err != nil {
		return nil, fmt.Errorf("running the ansible container: %w", err)
	}

	return &ansiblev1.RunPlaybookResponse{
		Ok:     result.ExitCode == 0,
		Output: redactValues(result.Stdout+result.Stderr, sensitive),
	}, nil
}

// tarFiles builds an in-memory tar archive, each file as 0600.
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

// minRedactLen avoids masking short, mundane values (ports, booleans, user
// names) that would make the output unreadable.
const minRedactLen = 8

// collectStrings returns every string contained in v, recursively.
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

// redactValues masks in out each value of values (and each of its lines, since
// Ansible may print a PEM block back line by line). The extra-vars carry
// secrets (certificates, role_id/secret_id): the playbook output travels up to
// the engine's errors and logs and must never contain them (the "no plaintext
// secret" rule).
func redactValues(out string, values []string) string {
	var needles []string
	for _, v := range values {
		for _, line := range strings.Split(v, "\n") {
			if line = strings.TrimSpace(line); len(line) >= minRedactLen {
				needles = append(needles, line)
			}
		}
	}
	// Longest first: a value contained in another must not prevent the longer
	// one from being masked.
	sort.Slice(needles, func(i, j int) bool { return len(needles[i]) > len(needles[j]) })
	for _, n := range needles {
		out = strings.ReplaceAll(out, n, "***")
	}
	return out
}
