// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestHelpListsDocumentedCommands vérifie que `genesis --help` fait apparaître
// toutes les commandes du tableau CLI de docs/02-architecture.md (critère
// d'acceptation du jalon J0).
func TestHelpListsDocumentedCommands(t *testing.T) {
	want := []string{"init", "modules", "validate", "plan", "apply", "status", "secrets", "destroy"}

	root := NewRootCmd()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("genesis --help : erreur inattendue : %v", err)
	}

	help := out.String()
	for _, name := range want {
		if !strings.Contains(help, name) {
			t.Errorf("genesis --help ne mentionne pas la commande %q\n--- sortie ---\n%s", name, help)
		}
	}
}

// TestStubCommandsFail vérifie que les commandes encore au stade de stub
// (jalons ultérieurs) renvoient une erreur explicite plutôt qu'un succès
// silencieux. `init`, `validate`, `secrets`, `modules`, `plan` et `apply`
// sont réellement implémentées (J1/J2/J3/J4) et testées ailleurs.
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
				t.Fatalf("genesis %s : succès inattendu, la commande n'est pourtant pas implémentée", strings.Join(args, " "))
			}
		})
	}
}
