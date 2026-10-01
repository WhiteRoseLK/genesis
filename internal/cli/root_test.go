// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestHelpListsDocumentedCommands checks that `genesis --help` shows every
// command of the CLI table in docs/02-architecture.md (M0 acceptance
// criterion).
func TestHelpListsDocumentedCommands(t *testing.T) {
	want := []string{"init", "modules", "validate", "plan", "apply", "status", "secrets", "destroy"}

	root := NewRootCmd()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("genesis --help: unexpected error: %v", err)
	}

	help := out.String()
	for _, name := range want {
		if !strings.Contains(help, name) {
			t.Errorf("genesis --help does not mention the %q command\n--- output ---\n%s", name, help)
		}
	}
}

// TestCommandsRequirePrerequisites checks that commands return an explicit error
// when run without required prerequisites (e.g. missing state or spec).
func TestCommandsRequirePrerequisites(t *testing.T) {
	cases := [][]string{
		{"status", "--state-dir", t.TempDir()},
		{"destroy", "-f", "nonexistent.yaml", "--auto-approve"},
	}

	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := NewRootCmd()
			out := &bytes.Buffer{}
			root.SetOut(out)
			root.SetErr(out)
			root.SetArgs(args)

			if err := root.Execute(); err == nil {
				t.Fatalf("genesis %s: unexpected success, expected error", strings.Join(args, " "))
			}
		})
	}
}
