// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/modulelock"
	"github.com/WhiteRoseLK/genesis/internal/scaffold"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
)

func newModulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "modules",
		Short: "Gère les modules installés (list, install, verify, scaffold)",
	}
	cmd.AddCommand(
		newModulesListCmd(),
		newModulesInstallCmd(),
		newModulesVerifyCmd(),
		newModulesScaffoldCmd(),
	)
	return cmd
}

func newModulesListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Liste les modules installés",
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		found, err := modulehost.Discover(modulehost.SearchPaths())
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if len(found) == 0 {
			_, err := fmt.Fprintln(out, "aucun module installé.")
			return err
		}
		for _, m := range found {
			if _, err := fmt.Fprintf(out, "%s\t%s\t%s\n", m.Name, m.Version, m.Dir); err != nil {
				return err
			}
		}
		return nil
	}
	return cmd
}

func newModulesInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <répertoire-source>",
		Short: "Compile et installe un module depuis son répertoire source (docs/10-ajouter-un-module.md)",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().String("lock-file", "genesis.lock", "chemin du fichier de verrouillage, à côté de la spec (doc 02)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		sourceDir := args[0]
		lockPath, err := cmd.Flags().GetString("lock-file")
		if err != nil {
			return err
		}

		manifest, err := sdk.LoadManifest(filepath.Join(sourceDir, "module.yaml"))
		if err != nil {
			return err
		}

		installRoot, err := modulehost.DefaultInstallDir()
		if err != nil {
			return err
		}
		destDir := filepath.Join(installRoot, manifest.Name, manifest.Version)
		if err := os.MkdirAll(destDir, 0o750); err != nil {
			return fmt.Errorf("création de %s : %w", destDir, err)
		}

		destBinary := filepath.Join(destDir, modulehost.BinaryName())
		build := exec.CommandContext(cmd.Context(), "go", "build", "-o", destBinary, ".")
		build.Dir = sourceDir
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := build.CombinedOutput(); err != nil {
			return fmt.Errorf("compilation de %s : %w\n%s", sourceDir, err, out)
		}

		if err := copyFile(filepath.Join(sourceDir, "module.yaml"), filepath.Join(destDir, "module.yaml")); err != nil {
			return err
		}
		if schemaPath := manifest.ConfigSchema; schemaPath != "" {
			if err := copyFile(filepath.Join(sourceDir, schemaPath), filepath.Join(destDir, schemaPath)); err != nil {
				return err
			}
		}
		if assetsDir := filepath.Join(sourceDir, "assets"); dirExists(assetsDir) {
			if err := copyDir(assetsDir, filepath.Join(destDir, "assets")); err != nil {
				return err
			}
		}

		fingerprint, err := modulehost.Fingerprint(destBinary)
		if err != nil {
			return err
		}

		lock, err := modulelock.Load(lockPath)
		if err != nil {
			return err
		}
		lock.Modules[manifest.Name] = modulelock.Entry{Version: manifest.Version, SHA256: fingerprint}
		if err := lock.Save(lockPath); err != nil {
			return err
		}

		_, err = fmt.Fprintf(cmd.OutOrStdout(), "module %s@%s installé dans %s (empreinte %s), %s mis à jour.\n",
			manifest.Name, manifest.Version, destDir, fingerprint, lockPath)
		return err
	}
	return cmd
}

func newModulesVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Vérifie l'empreinte des modules installés contre genesis.lock",
	}
	cmd.Flags().String("lock-file", "genesis.lock", "chemin du fichier de verrouillage")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		lockPath, err := cmd.Flags().GetString("lock-file")
		if err != nil {
			return err
		}
		lock, err := modulelock.Load(lockPath)
		if err != nil {
			return err
		}
		found, err := modulehost.Discover(modulehost.SearchPaths())
		if err != nil {
			return err
		}

		var failures []string
		for _, m := range found {
			fingerprint, err := modulehost.Fingerprint(m.BinaryPath)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s : %v", m.Name, err))
				continue
			}
			if err := lock.Verify(m.Name, m.Version, fingerprint); err != nil {
				failures = append(failures, err.Error())
			}
		}
		if len(failures) > 0 {
			return fmt.Errorf("module(s) refusé(s) :\n%s", strings.Join(failures, "\n"))
		}

		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%d module(s) vérifié(s), toutes les empreintes correspondent à %s.\n", len(found), lockPath)
		return err
	}
	return cmd
}

func newModulesScaffoldCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scaffold <nom>",
		Short: "Génère modules/<nom>/ (à exécuter depuis la racine du dépôt, docs/10-ajouter-un-module.md)",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().StringSlice("provides", nil, "fonctions fournies (ex. dns.zone/v1,dns.resolver/v1)")
	_ = cmd.MarkFlagRequired("provides")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		name := args[0]
		provides, err := cmd.Flags().GetStringSlice("provides")
		if err != nil {
			return err
		}
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir, err := scaffold.Generate(wd, name, provides)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "module %s généré dans %s (compile, ajouté au workspace Go).\n", name, dir)
		return err
	}
	return cmd
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("lecture de %s : %w", src, err)
	}
	defer func() { _ = in.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("création de %s : %w", filepath.Dir(dst), err)
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("écriture de %s : %w", dst, err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copie de %s vers %s : %w", src, dst, err)
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		return copyFile(path, target)
	})
}
