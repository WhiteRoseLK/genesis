// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"genesis/internal/modulehost"
	"genesis/internal/planner"
	"genesis/internal/resolver"
	"genesis/internal/secrets"
	"genesis/internal/spec"
	"genesis/internal/state"
)

// TestMigratesSecretsToKVCapability prouve docs/06-secrets-etat.md : dès
// que le module choisi pour la capacité "secrets" fournit secrets.kv/v1 et
// devient target_ready, chaque secret non-recovery du backend file est
// copié, vérifié par relecture, et le backend actif bascule — sans
// dépendre du vrai produit Vault (test-kv est un stockage en mémoire
// simulé, docs/03 §4 règle 7).
func TestMigratesSecretsToKVCapability(t *testing.T) {
	searchRoot := t.TempDir()
	installedKV := buildAndInstall(t, "test-kv", searchRoot)

	store := newTestSecretsStore(t)
	ctx := context.Background()

	// Deux secrets non-recovery à migrer, un secret recovery qui doit
	// rester en file (docs06 : "Les entrées Recovery: true restent dans file").
	mustEnsure(t, store, ctx, "app/password", secrets.GeneratePassword(), secrets.Meta{Kind: "password"})
	mustEnsure(t, store, ctx, "app/token", secrets.GenerateToken(), secrets.Meta{Kind: "token"})
	mustEnsure(t, store, ctx, "ca/root-key", secrets.GenerateEd25519Key(), secrets.Meta{Kind: "private-key", Recovery: true})

	env := &spec.Environment{
		Capabilities: map[string]spec.Capability{
			"secrets": {Module: "test-kv"},
		},
	}
	resolved, err := resolver.Resolve(env, []modulehost.Installed{installedKV})
	if err != nil {
		t.Fatalf("Resolve : %v", err)
	}
	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("Build : %v", err)
	}

	stateDir := t.TempDir()
	var logBuf bytes.Buffer
	e := New(stateDir, store)
	e.Logger = slog.New(slog.NewTextHandler(&logBuf, nil))
	if err := e.Run(ctx, resolved, plan); err != nil {
		t.Fatalf("Run : %v", err)
	}

	if !strings.Contains(logBuf.String(), "étape=secrets.migrate") {
		t.Errorf("la migration n'a jamais été déclenchée :\n%s", logBuf.String())
	}

	st, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("Load état : %v", err)
	}
	if st.SecretsBackend != "vault" {
		t.Errorf("SecretsBackend = %q, attendu \"vault\" après migration", st.SecretsBackend)
	}

	// Deuxième run : idempotent, ne re-déclenche pas la migration.
	var secondLog bytes.Buffer
	e2 := New(stateDir, store)
	e2.Logger = slog.New(slog.NewTextHandler(&secondLog, nil))
	if err := e2.Run(ctx, resolved, plan); err != nil {
		t.Fatalf("second Run : %v", err)
	}
	if strings.Contains(secondLog.String(), "étape=secrets.migrate") {
		t.Errorf("la migration a été rejouée alors que SecretsBackend est déjà \"vault\" :\n%s", secondLog.String())
	}
}

func mustEnsure(t *testing.T, store secrets.Store, ctx context.Context, ref string, gen secrets.Generator, meta secrets.Meta) {
	t.Helper()
	if err := store.Ensure(ctx, secrets.Ref(ref), gen, meta); err != nil {
		t.Fatalf("Ensure(%q) : %v", ref, err)
	}
}
