// SPDX-License-Identifier: Apache-2.0

// Package runner drives the seed's container runtime (docker or podman)
// through its command line — docs/02-architecture.md: "runners are exposed to
// modules as built-in functions" (core.container/v1, core.ansible/v1). It
// orchestrates the existing tool rather than re-implementing a Docker API
// client (ADR-002, the same logic applied to runners).
package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
)

// ContainerRuntime drives a docker/podman binary already installed on the seed
// (docs/04-spec.md: seed.container_runtime).
type ContainerRuntime struct {
	binary string
}

// DetectContainerRuntime looks for preferred ("docker", "podman") or, if empty
// or "auto", the first of the two found on the PATH.
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
	return nil, fmt.Errorf("no container runtime found among %v (install docker or podman)", candidates)
}

// Ping checks that the runtime's daemon answers (the binary alone is not
// enough: the docker client may be installed without a reachable daemon).
func (r *ContainerRuntime) Ping(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, r.binary, "info")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s info: %w\n%s", r.binary, err, stderr.String())
	}
	return nil
}

// Mount is a host -> container bind mount.
type Mount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

// RunOptions describes a container to start.
type RunOptions struct {
	Name    string
	Image   string
	Command []string
	Env     map[string]string
	Mounts  []Mount
	// Tmpfs: in-memory mount points (`--tmpfs` option), for files that must
	// never touch the seed's disk.
	Tmpfs []string
	// Stdin: sent to the container's standard input (`-i` option), in blocking
	// mode only.
	Stdin []byte
	// Files: files placed in the container before it starts (absolute path ->
	// content). They are copied into the container's layer, never written to
	// the seed's disk: blocking mode only.
	Files map[string][]byte
	// Collect: files read back from the container after it exits (absolute
	// paths), returned in RunResult.Collected. Blocking mode only.
	Collect []string
	// Detach: see the core.container/v1 RunRequest proto.
	Detach bool
}

// RunResult is the result of Run.
type RunResult struct {
	ContainerID string
	ExitCode    int
	Stdout      string
	Stderr      string
	// Collected: content of the files requested by RunOptions.Collect.
	Collected map[string][]byte
}

// Run starts a container. Not detached (default): blocks until it exits and
// returns its output. Detached: returns immediately with the ID.
func (r *ContainerRuntime) Run(ctx context.Context, opts RunOptions) (*RunResult, error) {
	if opts.Detach && (len(opts.Files) > 0 || len(opts.Collect) > 0) {
		return nil, errors.New("the Files and Collect options are only available in blocking mode (Detach=false)")
	}
	if len(opts.Files) > 0 || len(opts.Collect) > 0 {
		return r.runWithFiles(ctx, opts)
	}

	args := []string{"run"}
	if opts.Detach {
		args = append(args, "-d")
	} else {
		args = append(args, "--rm")
	}
	if opts.Stdin != nil && !opts.Detach {
		args = append(args, "-i")
	}
	args, env := containerArgs(args, opts)

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Env = env
	if opts.Stdin != nil && !opts.Detach {
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	if opts.Detach {
		if runErr != nil {
			return nil, fmt.Errorf("%s run -d %s: %w\n%s", r.binary, opts.Image, runErr, stderr.String())
		}
		return &RunResult{ContainerID: strings.TrimSpace(stdout.String())}, nil
	}

	exitCode, err := exitCodeOf(runErr)
	if err != nil {
		return nil, fmt.Errorf("%s run %s: %w\n%s", r.binary, opts.Image, err, stderr.String())
	}
	return &RunResult{ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

// containerArgs completes args (run/create) with the common options, then the
// image and the command, and returns the environment of the runtime process.
func containerArgs(args []string, opts RunOptions) ([]string, []string) {
	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}
	// `-e NAME` without a value: the runtime reads the value from its own
	// environment. The (possibly secret) values thus never appear on the
	// command line, which anyone can see through ps.
	env := os.Environ()
	for k, v := range opts.Env {
		args = append(args, "-e", k)
		env = append(env, k+"="+v)
	}
	for _, t := range opts.Tmpfs {
		args = append(args, "--tmpfs", t)
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
	return args, env
}

// exitCodeOf distinguishes "the container exited with a non-zero code" (not a
// runtime error) from a real failure to run the binary.
func exitCodeOf(runErr error) (int, error) {
	if runErr == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, runErr
}

// runWithFiles runs a blocking container while exchanging files through the
// container's layer: create, cp (in), start --attach, cp (out), rm. The files
// (keys, passwords) never go through a directory of the seed, and the
// container's internal UID does not matter.
func (r *ContainerRuntime) runWithFiles(ctx context.Context, opts RunOptions) (*RunResult, error) {
	args, env := containerArgs([]string{"create"}, opts)
	var stdout, stderr bytes.Buffer
	create := exec.CommandContext(ctx, r.binary, args...)
	create.Env = env
	create.Stdout = &stdout
	create.Stderr = &stderr
	if err := create.Run(); err != nil {
		return nil, fmt.Errorf("%s create %s: %w\n%s", r.binary, opts.Image, err, stderr.String())
	}
	id := strings.TrimSpace(stdout.String())
	defer func() { _ = r.Stop(context.WithoutCancel(ctx), id) }()

	if len(opts.Files) > 0 {
		archive, err := tarFiles(opts.Files)
		if err != nil {
			return nil, err
		}
		if err := r.exec(ctx, bytes.NewReader(archive), nil, "cp", "-", id+":/"); err != nil {
			return nil, err
		}
	}

	stdout.Reset()
	stderr.Reset()
	start := exec.CommandContext(ctx, r.binary, "start", "--attach", id)
	start.Stdout = &stdout
	start.Stderr = &stderr
	exitCode, err := exitCodeOf(start.Run())
	if err != nil {
		return nil, fmt.Errorf("%s start %s: %w\n%s", r.binary, opts.Image, err, stderr.String())
	}
	result := &RunResult{ContainerID: id, ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String()}
	if exitCode != 0 {
		return result, nil
	}

	result.Collected = map[string][]byte{}
	for _, path := range opts.Collect {
		var archive bytes.Buffer
		if err := r.exec(ctx, nil, &archive, "cp", id+":"+path, "-"); err != nil {
			return nil, err
		}
		content, err := firstTarFile(&archive)
		if err != nil {
			return nil, fmt.Errorf("reading %s in the container: %w", path, err)
		}
		result.Collected[path] = content
	}
	return result, nil
}

// exec runs a runtime subcommand (stdin/stdout optional).
func (r *ContainerRuntime) exec(ctx context.Context, stdin io.Reader, stdout io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w\n%s", r.binary, strings.Join(args[:1], " "), err, stderr.String())
	}
	return nil
}

// tarFiles builds an archive to extract at the container's root. Parent
// directories are created as 1777: the container's internal user, whoever it
// is, must be able to write its output files there. These permissions only
// apply to the container's layer, invisible to the seed's other users.
func tarFiles(files map[string][]byte) ([]byte, error) {
	paths := make([]string, 0, len(files))
	dirs := map[string]bool{}
	for p := range files {
		if !strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("file path %q: must be absolute", p)
		}
		paths = append(paths, p)
		for d := path.Dir(p); d != "/"; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	dirList := make([]string, 0, len(dirs))
	for d := range dirs {
		dirList = append(dirList, d)
	}
	sort.Strings(dirList) // parents before children
	sort.Strings(paths)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, d := range dirList {
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: strings.TrimPrefix(d, "/") + "/", Mode: 0o1777}); err != nil {
			return nil, err
		}
	}
	for _, p := range paths {
		content := files[p]
		if err := tw.WriteHeader(&tar.Header{Name: strings.TrimPrefix(p, "/"), Mode: 0o644, Size: int64(len(content))}); err != nil {
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

// firstTarFile returns the content of the first regular file of the archive
// (`docker cp <id>:<file> -` produces a single-file archive).
func firstTarFile(r io.Reader) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, fmt.Errorf("no file in the archive: %w", err)
		}
		if h.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
}

// Stop stops and removes a detached container. "docker stop" alone does not
// free the container name (bug found while building M6: fake-compute.DeleteVM
// followed by EnsureVM with the same name failed with "Conflict. The container
// name ... is already in use") — Stop must really mean "this container may go
// away", not just "paused": nothing calls Status after Stop to inspect it
// again.
func (r *ContainerRuntime) Stop(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, r.binary, "rm", "-f", containerID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s rm -f %s: %w\n%s", r.binary, containerID, err, stderr.String())
	}
	return nil
}

// InspectIP returns the container's IP address on its network (the first
// network found — enough as long as a container is attached to a single
// network, which is the case for everything the core starts today).
func (r *ContainerRuntime) InspectIP(ctx context.Context, containerID string) (string, error) {
	cmd := exec.CommandContext(ctx, r.binary, "inspect", "-f",
		"{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", containerID)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s inspect %s: %w\n%s", r.binary, containerID, err, stderr.String())
	}
	ip := strings.TrimSpace(stdout.String())
	if ip == "" {
		return "", fmt.Errorf("no IP address found for %s", containerID)
	}
	return ip, nil
}

// Status inspects a container's state.
func (r *ContainerRuntime) Status(ctx context.Context, containerID string) (state string, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, r.binary, "inspect", "--format", "{{.State.Status}} {{.State.ExitCode}}", containerID)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return "unknown", 0, fmt.Errorf("%s inspect %s: %w\n%s", r.binary, containerID, runErr, stderr.String())
	}
	fields := strings.Fields(strings.TrimSpace(stdout.String()))
	if len(fields) != 2 {
		return "unknown", 0, fmt.Errorf("unexpected output from %s inspect %s: %q", r.binary, containerID, stdout.String())
	}
	var code int
	if _, scanErr := fmt.Sscanf(fields[1], "%d", &code); scanErr != nil {
		return fields[0], 0, fmt.Errorf("invalid exit code in %q: %w", stdout.String(), scanErr)
	}
	return fields[0], code, nil
}
