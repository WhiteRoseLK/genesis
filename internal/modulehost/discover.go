// SPDX-License-Identifier: Apache-2.0

// Package modulehost découvre, lance et surveille les modules
// (docs/02-architecture.md : hôte de modules).
package modulehost

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
)

// SearchPaths retourne les répertoires de recherche des modules, dans
// l'ordre de priorité (docs/02-architecture.md).
func SearchPaths() []string {
	var paths []string
	if p := os.Getenv("GENESIS_MODULE_PATH"); p != "" {
		paths = append(paths, filepath.SplitList(p)...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".local", "share", "genesis", "modules"))
	}
	paths = append(paths, "/usr/lib/genesis/modules")
	return paths
}

// BinaryName est le nom attendu du binaire d'un module pour l'OS/l'archi
// courants (docs/02-architecture.md : <nom>/<version>/module-<os>-<arch>).
func BinaryName() string {
	return fmt.Sprintf("module-%s-%s", runtime.GOOS, runtime.GOARCH)
}

// DefaultInstallDir est le répertoire utilisateur où `genesis modules
// install` écrit un module (docs/02-architecture.md :
// ~/.local/share/genesis/modules).
func DefaultInstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("détermination du répertoire personnel : %w", err)
	}
	return filepath.Join(home, ".local", "share", "genesis", "modules"), nil
}

// Installed décrit un module découvert sur disque.
type Installed struct {
	Name         string
	Version      string
	Dir          string // <search-path>/<nom>/<version>
	ManifestPath string
	BinaryPath   string
	Manifest     *sdk.ManifestFile
}

// Discover recense les modules installés sous les répertoires de recherche :
// <nom>/<version>/{module.yaml, module-<os>-<arch>, assets/}.
func Discover(searchPaths []string) ([]Installed, error) {
	var found []Installed
	for _, root := range searchPaths {
		names, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lecture de %s : %w", root, err)
		}
		for _, nameEntry := range names {
			if !nameEntry.IsDir() {
				continue
			}
			nameDir := filepath.Join(root, nameEntry.Name())
			versions, err := os.ReadDir(nameDir)
			if err != nil {
				return nil, fmt.Errorf("lecture de %s : %w", nameDir, err)
			}
			for _, versionEntry := range versions {
				if !versionEntry.IsDir() {
					continue
				}
				dir := filepath.Join(nameDir, versionEntry.Name())
				manifestPath := filepath.Join(dir, "module.yaml")
				if _, err := os.Stat(manifestPath); err != nil {
					continue // pas un module valide, on l'ignore
				}
				manifest, err := sdk.LoadManifest(manifestPath)
				if err != nil {
					return nil, fmt.Errorf("manifest invalide dans %s : %w", manifestPath, err)
				}
				found = append(found, Installed{
					Name:         nameEntry.Name(),
					Version:      versionEntry.Name(),
					Dir:          dir,
					ManifestPath: manifestPath,
					BinaryPath:   filepath.Join(dir, BinaryName()),
					Manifest:     manifest,
				})
			}
		}
	}
	return found, nil
}
