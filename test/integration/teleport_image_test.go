// SPDX-License-Identifier: Apache-2.0

//go:build docker

package integration

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Teleport test image, built locally instead of being downloaded (#51):
// gravitational only publishes its images on public.ecr.aws, whose anonymous
// per-IP quota, shared between GitHub runners, was blocking CI; and since v16
// they are "distroless" (no shell), which prevents the agent from opening an
// SSH session. The image therefore assembles a Debian base pinned by digest
// (pulled ahead of time by `make pull-images`) and the teleport/tctl binaries
// of the official archive, verified by checksum: the same major version as the
// one the module installs (playbooks/install_teleport.yml, stable/v17
// channel).
const (
	teleportVersion   = "17.7.29"
	teleportBaseImage = "debian:13.7-slim@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a"
)

// Checksums published next to each archive (…-bin.tar.gz.sha256 on
// cdn.teleport.dev).
var teleportArchiveSHA256 = map[string]string{
	"amd64": "ffe83412cc91dfeef533ac2627021cd20f9d017d6b1dbe7db3d1bebfcb17bfdf",
	"arm64": "5d296d08be64d3d2a318aacb475a19e6246ebef20a4139f312d31c16d54d55ba",
}

var (
	teleportImageOnce sync.Once
	teleportImageRef  string
	teleportImageErr  error
)

// teleportTestImage returns the name of the test image, built on the first
// call. Its tag derives from its inputs (archive checksum, Dockerfile, hence
// the base image): an image already built with the same inputs is reused as
// is.
func teleportTestImage(t *testing.T) string {
	t.Helper()
	teleportImageOnce.Do(func() {
		teleportImageRef, teleportImageErr = buildTeleportImage()
	})
	if teleportImageErr != nil {
		t.Fatalf("teleport test image: %v", teleportImageErr)
	}
	return teleportImageRef
}

func buildTeleportImage() (string, error) {
	sum, ok := teleportArchiveSHA256[runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("unsupported architecture %s (amd64, arm64)", runtime.GOARCH)
	}
	dockerfile := "FROM " + teleportBaseImage + "\nCOPY teleport tctl /usr/local/bin/\nENTRYPOINT [\"teleport\"]\n"
	inputs := sha256.Sum256([]byte(sum + dockerfile))
	ref := fmt.Sprintf("genesis-test/teleport:%s-%s", teleportVersion, hex.EncodeToString(inputs[:6]))
	if exec.Command("docker", "image", "inspect", ref).Run() == nil {
		return ref, nil
	}

	archive, err := teleportArchive(sum)
	if err != nil {
		return "", err
	}
	buildDir, err := os.MkdirTemp("", "genesis-teleport-image-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(buildDir) }()
	if err := extractTeleportBinaries(archive, buildDir, "teleport", "tctl"); err != nil {
		return "", fmt.Errorf("extracting %s: %w", archive, err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return "", err
	}
	if out, err := exec.Command("docker", "build", "-q", "-t", ref, buildDir).CombinedOutput(); err != nil {
		return "", fmt.Errorf("docker build: %w: %s", err, out)
	}
	return ref, nil
}

// teleportArchive returns the path of the official archive, downloaded from
// cdn.teleport.dev unless it is already in the cache (GENESIS_TEST_CACHE,
// otherwise the user cache; kept by CI between runs). Any archive whose
// checksum differs is rejected.
func teleportArchive(sum string) (string, error) {
	dir := os.Getenv("GENESIS_TEST_CACHE")
	if dir == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(userCache, "genesis-test")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("teleport-v%s-linux-%s-bin.tar.gz", teleportVersion, runtime.GOARCH)
	path := filepath.Join(dir, name)
	if got, err := fileSHA256(path); err == nil && got == sum {
		return path, nil
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get("https://cdn.teleport.dev/" + name)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: %s", name, resp.Status)
	}
	tmp, err := os.CreateTemp(dir, name+".*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(tmp, h), resp.Body)
	if closeErr := tmp.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return "", fmt.Errorf("downloading %s: %w", name, copyErr)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return "", fmt.Errorf("%s: sha256 checksum %s, expected %s", name, got, sum)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractTeleportBinaries copies teleport/<name> from the archive into dir,
// for each requested name.
func extractTeleportBinaries(archive, dir string, names ...string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	wanted := map[string]string{}
	for _, n := range names {
		wanted["teleport/"+n] = n
	}
	tr := tar.NewReader(gz)
	for len(wanted) > 0 {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		n, ok := wanted[hdr.Name]
		if !ok || hdr.Typeflag != tar.TypeReg {
			continue
		}
		out, err := os.OpenFile(filepath.Join(dir, n), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		if closeErr := out.Close(); copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			return copyErr
		}
		delete(wanted, hdr.Name)
	}
	if len(wanted) > 0 {
		return fmt.Errorf("files missing from the archive: %v", wanted)
	}
	return nil
}
