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

// TestStubCommandsFail checks that the commands still at the stub stage (later
// milestones) return an explicit error rather than a silent success. `init`,
// `validate`, `secrets`, `modules`, `plan` and `apply` are really implemented
// (M1/M2/M3/M4) and tested elsewhere.
func TestStubCommandsFail(t *testing.T) {
	cases := [][]string{
		{"status"},
		{"destroy", "-f", "env.yaml"},
	}

	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := NewRootCmd()
			out := &bytes.Buffer{}
			root.SetOut(out)
			root.SetErr(out)
			root.SetArgs(args)

			if err := root.Execute(); err == nil {
				t.Fatalf("genesis %s: unexpected success, although the command is not implemented", strings.Join(args, " "))
			}
		})
	}
}
