// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLISplit runs the CLI and returns stdout and stderr separately.
func runCLISplit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := NewRootCmd()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func writeStateDirSpec(t *testing.T, stateDir string) string {
	t.Helper()
	specFile := filepath.Join(t.TempDir(), "spec.yaml")
	content := `apiVersion: genesis/v1alpha1
kind: Environment
metadata: { name: lab }
network: { cidr: 10.10.0.0/24, gateway: 10.10.0.1, pool: 10.10.0.50-10.10.0.99, domain: lab.internal }
seed: { address: 10.10.0.10, state_dir: ` + stateDir + ` }
capabilities:
  test-a: { module: test-a }
`
	if err := os.WriteFile(specFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return specFile
}

// Regression for #142: status, init and clean must not fall back silently to
// the default state directory when the spec given with -f cannot be loaded.
func TestStateCommandsRejectUnusableSpec(t *testing.T) {
	invalid := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(invalid, []byte("not: [a valid spec"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.yaml")

	for _, cmd := range [][]string{{"status"}, {"init"}, {"clean", "--auto-approve"}} {
		for name, file := range map[string]string{"missing": missing, "invalid": invalid} {
			t.Run(cmd[0]+"/"+name, func(t *testing.T) {
				_, err := runCLI(t, append(append([]string{}, cmd...), "-f", file)...)
				if err == nil {
					t.Fatalf("%v -f %s: unexpected success", cmd, name)
				}
				if !strings.Contains(err.Error(), filepath.Base(file)) {
					t.Errorf("error = %q, want it to name the spec", err)
				}
			})
		}
	}
}

// An explicit --state-dir keeps precedence: the spec is not even consulted.
func TestInitExplicitStateDirIgnoresUnusableSpec(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	if _, err := runCLI(t, "init", "-f", missing, "--state-dir", stateDir); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "master.key")); err != nil {
		t.Errorf("master key not created in the explicit directory: %v", err)
	}
}

// #143: the resolved absolute state directory is announced on stderr, only
// when it comes from a spec.
func TestStateDirAnnouncedOnlyWhenFromSpec(t *testing.T) {
	// A relative state_dir in the spec resolves against the working directory.
	work := t.TempDir()
	t.Chdir(work)
	specFile := writeStateDirSpec(t, "rel-state")

	stdout, stderr, err := runCLISplit(t, "init", "-f", specFile)
	if err != nil {
		t.Fatalf("init -f: %v", err)
	}
	suffix := " (from " + specFile + ")"
	got := strings.TrimSpace(stderr)
	if !strings.HasPrefix(got, "state directory: ") || !strings.HasSuffix(got, suffix) {
		t.Fatalf("stderr = %q, want the state directory line", stderr)
	}
	announced := strings.TrimSuffix(strings.TrimPrefix(got, "state directory: "), suffix)
	if !filepath.IsAbs(announced) {
		t.Errorf("announced path %q is not absolute", announced)
	}
	if !strings.HasSuffix(announced, string(filepath.Separator)+"rel-state") {
		t.Errorf("announced path = %q, want it to end with rel-state", announced)
	}
	if strings.Contains(stdout, "state directory: ") {
		t.Errorf("the announcement must go to stderr, stdout = %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(work, "rel-state", "master.key")); err != nil {
		t.Errorf("master key not created in the resolved directory: %v", err)
	}

	for _, args := range [][]string{
		{"status", "-f", specFile},
		{"secrets", "list", "-f", specFile},
	} {
		// status may fail later (the module is not installed); the
		// announcement precedes that.
		if _, stderr, _ := runCLISplit(t, args...); !strings.Contains(stderr, "state directory: ") {
			t.Errorf("%v: stderr = %q; want the announcement", args, stderr)
		}
	}

	explicit := filepath.Join(t.TempDir(), "explicit")
	if _, stderr, err := runCLISplit(t, "init", "--state-dir", explicit); err != nil || strings.Contains(stderr, "state directory:") {
		t.Errorf("explicit --state-dir: stderr = %q, err = %v; want no announcement", stderr, err)
	}
	if _, stderr, err := runCLISplit(t, "init", "-f", specFile, "--state-dir", explicit); err != nil || strings.Contains(stderr, "state directory:") {
		t.Errorf("explicit --state-dir with -f: stderr = %q, err = %v; want no announcement", stderr, err)
	}
	if _, stderr, err := runCLISplit(t, "status", "--state-dir", explicit); err != nil || strings.Contains(stderr, "state directory:") {
		t.Errorf("status --state-dir: stderr = %q, err = %v; want no announcement", stderr, err)
	}
}
