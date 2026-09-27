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
		Short: "Manage installed modules (list, install, verify, scaffold)",
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
		Short: "List installed modules",
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		found, err := modulehost.Discover(modulehost.SearchPaths())
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if len(found) == 0 {
			_, err := fmt.Fprintln(out, "no module installed.")
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
		Use:   "install <source-directory>",
		Short: "Build and install a module from its source directory (docs/10-adding-a-module.md)",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().String("lock-file", "genesis.lock", "path of the lock file, next to the spec (doc 02)")
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
			return fmt.Errorf("creating %s: %w", destDir, err)
		}

		destBinary := filepath.Join(destDir, modulehost.BinaryName())
		build := exec.CommandContext(cmd.Context(), "go", "build", "-o", destBinary, ".")
		build.Dir = sourceDir
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := build.CombinedOutput(); err != nil {
			return fmt.Errorf("compilation de %s: %w\n%s", sourceDir, err, out)
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

		_, err = fmt.Fprintf(cmd.OutOrStdout(), "module %s@%s installed in %s (digest %s), %s updated.\n",
			manifest.Name, manifest.Version, destDir, fingerprint, lockPath)
		return err
	}
	return cmd
}

func newModulesVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify the digests of installed modules against genesis.lock",
	}
	cmd.Flags().String("lock-file", "genesis.lock", "path of the lock file")
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
				failures = append(failures, fmt.Sprintf("%s: %v", m.Name, err))
				continue
			}
			if err := lock.Verify(m.Name, m.Version, fingerprint); err != nil {
				failures = append(failures, err.Error())
			}
		}
		if len(failures) > 0 {
			return fmt.Errorf("module(s) refused:\n%s", strings.Join(failures, "\n"))
		}

		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%d module(s) verified, every digest matches %s.\n", len(found), lockPath)
		return err
	}
	return cmd
}

func newModulesScaffoldCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scaffold <name>",
		Short: "Generate modules/<name>/ (run from the repository root, docs/10-adding-a-module.md)",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().StringSlice("provides", nil, "provided functions (e.g. dns.zone/v1,dns.resolver/v1)")
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
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "module %s generated in %s (builds, added to the Go workspace).\n", name, dir)
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
		return fmt.Errorf("lecture de %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying %s to %s: %w", src, dst, err)
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
