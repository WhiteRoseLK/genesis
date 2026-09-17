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
		t.Fatalf("genesis init : %v", err)
	}
	if !strings.Contains(out, "Clé maîtresse générée") {
		t.Errorf("sortie du premier init = %q, attendu la mention de génération de la clé", out)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "master.key")); err != nil {
		t.Errorf("master.key absent : %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "state.json")); err != nil {
		t.Errorf("state.json absent : %v", err)
	}
}

func TestInitIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")

	if _, err := runCLI(t, "init", "--state-dir", stateDir); err != nil {
		t.Fatalf("premier init : %v", err)
	}
	keyBefore, err := os.ReadFile(filepath.Join(stateDir, "master.key"))
	if err != nil {
		t.Fatalf("lecture de master.key : %v", err)
	}

	out, err := runCLI(t, "init", "--state-dir", stateDir)
	if err != nil {
		t.Fatalf("second init : %v", err)
	}
	if strings.Contains(out, "Clé maîtresse générée") {
		t.Errorf("le second init a réaffiché la clé maîtresse : %q", out)
	}
	if !strings.Contains(out, "Déjà initialisé") {
		t.Errorf("sortie du second init = %q, attendu la mention \"Déjà initialisé\"", out)
	}

	keyAfter, err := os.ReadFile(filepath.Join(stateDir, "master.key"))
	if err != nil {
		t.Fatalf("lecture de master.key après second init : %v", err)
	}
	if string(keyBefore) != string(keyAfter) {
		t.Error("la clé maîtresse a changé après un second init")
	}
}
