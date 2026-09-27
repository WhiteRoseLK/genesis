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

// Image de test Teleport, construite localement au lieu d'être téléchargée
// (#51) : gravitational ne publie ses images que sur public.ecr.aws, dont le
// quota anonyme par adresse IP, partagé entre les runners GitHub, bloquait
// la CI ; et depuis la v16 elles sont « distroless » (sans shell), ce qui
// empêche l'agent d'ouvrir une session SSH. L'image assemble donc une base
// Debian épinglée par empreinte (tirée d'avance par `make pull-images`) et
// les binaires teleport/tctl de l'archive officielle, vérifiée par somme de
// contrôle : même version majeure que celle qu'installe le module
// (playbooks/install_teleport.yml, canal stable/v17).
const (
	teleportVersion   = "17.7.29"
	teleportBaseImage = "debian:13.7-slim@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a"
)

// Sommes publiées à côté de chaque archive (…-bin.tar.gz.sha256 sur
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

// teleportTestImage renvoie le nom de l'image de test, construite au premier
// appel. Son étiquette dérive de ses entrées (somme de l'archive,
// Dockerfile, donc image de base) : une image déjà construite avec les
// mêmes entrées est réutilisée telle quelle.
func teleportTestImage(t *testing.T) string {
	t.Helper()
	teleportImageOnce.Do(func() {
		teleportImageRef, teleportImageErr = buildTeleportImage()
	})
	if teleportImageErr != nil {
		t.Fatalf("image de test teleport : %v", teleportImageErr)
	}
	return teleportImageRef
}

func buildTeleportImage() (string, error) {
	sum, ok := teleportArchiveSHA256[runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("architecture %s non prise en charge (amd64, arm64)", runtime.GOARCH)
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
		return "", fmt.Errorf("extraction de %s : %w", archive, err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return "", err
	}
	if out, err := exec.Command("docker", "build", "-q", "-t", ref, buildDir).CombinedOutput(); err != nil {
		return "", fmt.Errorf("docker build : %w : %s", err, out)
	}
	return ref, nil
}

// teleportArchive renvoie le chemin de l'archive officielle, téléchargée
// depuis cdn.teleport.dev si elle n'est pas déjà dans le cache
// (GENESIS_TEST_CACHE, sinon le cache utilisateur ; conservé par la CI
// entre deux exécutions). Toute archive dont la somme diffère est rejetée.
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
		return "", fmt.Errorf("téléchargement de %s : %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("téléchargement de %s : %s", name, resp.Status)
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
		return "", fmt.Errorf("téléchargement de %s : %w", name, copyErr)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return "", fmt.Errorf("%s : somme sha256 %s, attendue %s", name, got, sum)
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

// extractTeleportBinaries copie teleport/<nom> de l'archive dans dir, pour
// chaque nom demandé.
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
		return fmt.Errorf("fichiers absents de l'archive : %v", wanted)
	}
	return nil
}
