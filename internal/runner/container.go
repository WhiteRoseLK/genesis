// SPDX-License-Identifier: Apache-2.0

// Package runner pilote le runtime de conteneurs (docker ou podman) de la
// graine en ligne de commande — docs/02-architecture.md : "les runners sont
// exposés aux modules comme fonctions intégrées" (core.container/v1,
// core.ansible/v1). Orchestrer l'outil existant plutôt que réimplémenter un
// client de l'API Docker (ADR-002, même logique appliquée aux runners).
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

// Ping vérifie que le démon du runtime répond (le binaire seul ne suffit pas :
// le client docker peut être installé sans démon joignable).
func (r *ContainerRuntime) Ping(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, r.binary, "info")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s info : %w\n%s", r.binary, err, stderr.String())
	}
	return nil
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
	// Tmpfs : points de montage en mémoire (option `--tmpfs`), pour les
	// fichiers qui ne doivent jamais toucher le disque de la graine.
	Tmpfs []string
	// Stdin : envoyé sur l'entrée standard du conteneur (option `-i`),
	// seulement en mode bloquant.
	Stdin []byte
	// Files : fichiers déposés dans le conteneur avant son démarrage (chemin
	// absolu -> contenu). Ils sont copiés dans la couche du conteneur, jamais
	// écrits sur le disque de la graine : réservé au mode bloquant.
	Files map[string][]byte
	// Collect : fichiers relus dans le conteneur après sa fin (chemins
	// absolus), renvoyés dans RunResult.Collected. Mode bloquant seulement.
	Collect []string
	// Detach : voir docs/proto core.container/v1 RunRequest.
	Detach bool
}

// RunResult est le résultat de Run.
type RunResult struct {
	ContainerID string
	ExitCode    int
	Stdout      string
	Stderr      string
	// Collected : contenu des fichiers demandés par RunOptions.Collect.
	Collected map[string][]byte
}

// Run démarre un conteneur. Non détaché (par défaut) : bloque jusqu'à sa fin
// et renvoie sa sortie. Détaché : rend la main immédiatement avec l'ID.
func (r *ContainerRuntime) Run(ctx context.Context, opts RunOptions) (*RunResult, error) {
	if opts.Detach && (len(opts.Files) > 0 || len(opts.Collect) > 0) {
		return nil, errors.New("les options Files et Collect ne sont possibles qu'en mode bloquant (Detach=false)")
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
			return nil, fmt.Errorf("%s run -d %s : %w\n%s", r.binary, opts.Image, runErr, stderr.String())
		}
		return &RunResult{ContainerID: strings.TrimSpace(stdout.String())}, nil
	}

	exitCode, err := exitCodeOf(runErr)
	if err != nil {
		return nil, fmt.Errorf("%s run %s : %w\n%s", r.binary, opts.Image, err, stderr.String())
	}
	return &RunResult{ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

// containerArgs complète args (run/create) avec les options communes, puis
// l'image et la commande, et renvoie l'environnement du processus runtime.
func containerArgs(args []string, opts RunOptions) ([]string, []string) {
	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}
	// `-e NOM` sans valeur : le runtime lit la valeur dans son propre
	// environnement. Les valeurs (potentiellement secrètes) n'apparaissent
	// ainsi jamais dans la ligne de commande, visible de tous via ps.
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

// exitCodeOf distingue « le conteneur a fini avec un code non nul » (pas une
// erreur du runtime) d'un vrai échec d'exécution du binaire.
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

// runWithFiles exécute un conteneur bloquant en échangeant des fichiers par
// la couche du conteneur : create, cp (entrée), start --attach, cp (sortie),
// rm. Les fichiers (clés, mots de passe) ne passent jamais par un répertoire
// de la graine, et l'UID interne du conteneur n'a pas d'importance.
func (r *ContainerRuntime) runWithFiles(ctx context.Context, opts RunOptions) (*RunResult, error) {
	args, env := containerArgs([]string{"create"}, opts)
	var stdout, stderr bytes.Buffer
	create := exec.CommandContext(ctx, r.binary, args...)
	create.Env = env
	create.Stdout = &stdout
	create.Stderr = &stderr
	if err := create.Run(); err != nil {
		return nil, fmt.Errorf("%s create %s : %w\n%s", r.binary, opts.Image, err, stderr.String())
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
		return nil, fmt.Errorf("%s start %s : %w\n%s", r.binary, opts.Image, err, stderr.String())
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
			return nil, fmt.Errorf("lecture de %s dans le conteneur : %w", path, err)
		}
		result.Collected[path] = content
	}
	return result, nil
}

// exec lance une sous-commande du runtime (stdin/stdout facultatifs).
func (r *ContainerRuntime) exec(ctx context.Context, stdin io.Reader, stdout io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s : %w\n%s", r.binary, strings.Join(args[:1], " "), err, stderr.String())
	}
	return nil
}

// tarFiles construit une archive à extraire à la racine du conteneur. Les
// répertoires parents sont créés en 1777 : l'utilisateur interne du
// conteneur, quel qu'il soit, doit pouvoir y écrire ses fichiers de sortie.
// Ces droits ne concernent que la couche du conteneur, invisible des autres
// utilisateurs de la graine.
func tarFiles(files map[string][]byte) ([]byte, error) {
	paths := make([]string, 0, len(files))
	dirs := map[string]bool{}
	for p := range files {
		if !strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("chemin de fichier %q : absolu attendu", p)
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
	sort.Strings(dirList) // parents avant enfants
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

// firstTarFile renvoie le contenu du premier fichier ordinaire de l'archive
// (`docker cp <id>:<fichier> -` produit une archive d'un seul fichier).
func firstTarFile(r io.Reader) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, fmt.Errorf("aucun fichier dans l'archive : %w", err)
		}
		if h.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
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
