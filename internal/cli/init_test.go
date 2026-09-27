// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	root := NewRootCmd()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), err
}

func TestInitCreatesMasterKeyAndState(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")

	out, err := runCLI(t, "init", "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("genesis init: %v", err)
	}
	if !strings.Contains(out, "Master key generated") {
		t.Errorf("output of the first init = %q, want the key generation message", out)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "master.key")); err != nil {
		t.Errorf("master.key absent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "state.json")); err != nil {
		t.Errorf("state.json absent: %v", err)
	}
}

func TestInitIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")

	if _, err := runCLI(t, "init", "--state-dir", stateDir); err != nil {
		t.Fatalf("first init: %v", err)
	}
	keyBefore, err := os.ReadFile(filepath.Join(stateDir, "master.key"))
	if err != nil {
		t.Fatalf("lecture de master.key: %v", err)
	}

	out, err := runCLI(t, "init", "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("second init: %v", err)
	}
	if strings.Contains(out, "Master key generated") {
		t.Errorf("the second init displayed the master key again: %q", out)
	}
	if !strings.Contains(out, "Already initialised") {
		t.Errorf("output of the second init = %q, want \"Already initialised\"", out)
	}

	keyAfter, err := os.ReadFile(filepath.Join(stateDir, "master.key"))
	if err != nil {
		t.Fatalf("reading master.key after the second init: %v", err)
	}
	if string(keyBefore) != string(keyAfter) {
		t.Error("the master key changed after a second init")
	}
}
