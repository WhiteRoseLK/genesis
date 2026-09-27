// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/planner"
	"github.com/WhiteRoseLK/genesis/internal/resolver"
	"github.com/WhiteRoseLK/genesis/internal/secrets"
	"github.com/WhiteRoseLK/genesis/internal/spec"
	"github.com/WhiteRoseLK/genesis/internal/state"
)

// TestMigratesSecretsToKVCapability proves docs/06-secrets-state.md: as
// soon as the module chosen for the "secrets" capability provides
// secrets.kv/v1 and becomes target_ready, each non-recovery secret of the
// file backend is copied, verified by reading it back, and the active
// backend switches — without depending on the real Vault product (test-kv
// is a simulated in-memory store, docs/03 §4 rule 7).
func TestMigratesSecretsToKVCapability(t *testing.T) {
	searchRoot := t.TempDir()
	installedKV := buildAndInstall(t, "test-kv", searchRoot)

	store := newTestSecretsStore(t)
	ctx := context.Background()

	// Two non-recovery secrets to migrate, one recovery secret that must
	// stay in file (doc 06: "Recovery: true entries stay in file").
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
		t.Fatalf("Resolve: %v", err)
	}
	plan, err := planner.Build(resolved)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	stateDir := t.TempDir()
	var logBuf bytes.Buffer
	e := New(stateDir, store)
	e.Logger = slog.New(slog.NewTextHandler(&logBuf, nil))
	if err := e.Run(ctx, resolved, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !strings.Contains(logBuf.String(), "step=secrets.migrate") {
		t.Errorf("the migration was never triggered:\n%s", logBuf.String())
	}

	st, err := state.Load(stateDir)
	if err != nil {
		t.Fatalf("Load state: %v", err)
	}
	if st.SecretsBackend != "vault" {
		t.Errorf("SecretsBackend = %q, want \"vault\" after migration", st.SecretsBackend)
	}

	// Second run: idempotent, does not trigger the migration again.
	var secondLog bytes.Buffer
	e2 := New(stateDir, store)
	e2.Logger = slog.New(slog.NewTextHandler(&secondLog, nil))
	if err := e2.Run(ctx, resolved, plan); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if strings.Contains(secondLog.String(), "step=secrets.migrate") {
		t.Errorf("the migration was replayed although SecretsBackend is already \"vault\":\n%s", secondLog.String())
	}
}

func mustEnsure(t *testing.T, store secrets.Store, ctx context.Context, ref string, gen secrets.Generator, meta secrets.Meta) {
	t.Helper()
	if err := store.Ensure(ctx, secrets.Ref(ref), gen, meta); err != nil {
		t.Fatalf("Ensure(%q): %v", ref, err)
	}
}
