// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"

	secretskvv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/secrets/kv/v1"
)

type mockKVClient struct {
	mu     sync.Mutex
	values map[string]string
}

func newMockKVClient() *mockKVClient {
	return &mockKVClient{values: make(map[string]string)}
}

func (m *mockKVClient) Read(_ context.Context, in *secretskvv1.ReadRequest, _ ...grpc.CallOption) (*secretskvv1.ReadResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	val, ok := m.values[in.GetRef()]
	return &secretskvv1.ReadResponse{Value: val, Found: ok}, nil
}

func (m *mockKVClient) Write(_ context.Context, in *secretskvv1.WriteRequest, _ ...grpc.CallOption) (*secretskvv1.WriteResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[in.GetRef()] = in.GetValue()
	return &secretskvv1.WriteResponse{}, nil
}

func (m *mockKVClient) List(_ context.Context, in *secretskvv1.ListRequest, _ ...grpc.CallOption) (*secretskvv1.ListResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var refs []string
	prefix := in.GetPrefix()
	for k := range m.values {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			refs = append(refs, k)
		}
	}
	return &secretskvv1.ListResponse{Refs: refs}, nil
}

func TestRouterFileMode(t *testing.T) {
	fs := newTestStore(t)
	r := NewRouter(fs)
	ctx := context.Background()

	if got := r.Backend(); got != "file" {
		t.Fatalf("Backend = %q, want \"file\"", got)
	}
	if r.FileStore() != fs {
		t.Fatalf("FileStore pointer mismatch")
	}

	ref := Ref("app/password")
	if err := r.Ensure(ctx, ref, GeneratePassword(), Meta{Owner: "app", Kind: "password"}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// Must be stored in FileStore
	got, err := fs.Get(ctx, ref)
	if err != nil {
		t.Fatalf("FileStore.Get: %v", err)
	}
	if got.ExposeSecret() == "" {
		t.Fatalf("empty secret stored in FileStore")
	}

	routerGot, err := r.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Router.Get: %v", err)
	}
	if routerGot.ExposeSecret() != got.ExposeSecret() {
		t.Fatalf("Router.Get = %q, want %q", routerGot.ExposeSecret(), got.ExposeSecret())
	}
}

func TestRouterSwitchToVault(t *testing.T) {
	fs := newTestStore(t)
	r := NewRouter(fs)
	ctx := context.Background()

	// 1. Write a recovery secret and a pre-migration secret in file mode.
	recRef := Ref("ca/root-key")
	if err := r.Put(ctx, recRef, NewSecret("root-key-content"), Meta{Owner: "step-ca", Kind: "private-key", Recovery: true}); err != nil {
		t.Fatalf("Put recovery secret: %v", err)
	}
	preRef := Ref("app/pre-secret")
	if err := r.Put(ctx, preRef, NewSecret("pre-val"), Meta{Owner: "app", Kind: "password"}); err != nil {
		t.Fatalf("Put pre secret: %v", err)
	}

	// 2. Switch to Vault with mock KV client.
	kv := newMockKVClient()
	// Simulate migrated secret in Vault:
	kv.values[string(preRef)] = "pre-val"

	r.SwitchToVault(kv)

	if got := r.Backend(); got != "vault" {
		t.Fatalf("Backend = %q, want \"vault\"", got)
	}
	if !fs.ReadOnly() {
		t.Fatalf("FileStore was not switched to read-only")
	}

	// 3. Direct writes to FileStore must now fail with ErrReadOnly.
	if err := fs.Put(ctx, Ref("leak/file"), NewSecret("val"), Meta{}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("direct FileStore.Put in read-only mode = %v, want ErrReadOnly", err)
	}

	// 4. Ensure a new secret through Router: must write to Vault, not FileStore.
	newRef := Ref("teleport/token")
	if err := r.Ensure(ctx, newRef, GenerateToken(), Meta{Owner: "teleport", Kind: "token"}); err != nil {
		t.Fatalf("Router.Ensure in vault mode: %v", err)
	}

	// Must exist in Vault
	if _, ok := kv.values[string(newRef)]; !ok {
		t.Fatalf("secret %q was not written to Vault", newRef)
	}
	// Must NOT exist in FileStore
	if _, err := fs.Get(ctx, newRef); err == nil {
		t.Fatalf("secret %q was written to FileStore, expected not found", newRef)
	}

	// 5. Read back through Router.Get:
	// a) Secret from Vault
	gotNew, err := r.Get(ctx, newRef)
	if err != nil {
		t.Fatalf("Router.Get(%q): %v", newRef, err)
	}
	if gotNew.ExposeSecret() != kv.values[string(newRef)] {
		t.Fatalf("got %q, want %q", gotNew.ExposeSecret(), kv.values[string(newRef)])
	}

	// b) Recovery secret from FileStore
	gotRec, err := r.Get(ctx, recRef)
	if err != nil {
		t.Fatalf("Router.Get recovery secret: %v", err)
	}
	if gotRec.ExposeSecret() != "root-key-content" {
		t.Fatalf("got recovery %q, want \"root-key-content\"", gotRec.ExposeSecret())
	}

	// 6. Router.Ensure on existing secret is idempotent (does not regenerate).
	calls := 0
	err = r.Ensure(ctx, newRef, func() (Secret, error) {
		calls++
		return NewSecret("regen"), nil
	}, Meta{Owner: "teleport", Kind: "token"})
	if err != nil {
		t.Fatalf("idempotent Ensure on vault secret: %v", err)
	}
	if calls != 0 {
		t.Fatalf("generator called %d times for existing vault secret", calls)
	}

	// Ensure on existing recovery secret is idempotent.
	err = r.Ensure(ctx, recRef, func() (Secret, error) {
		calls++
		return NewSecret("regen"), nil
	}, Meta{Owner: "step-ca", Kind: "private-key", Recovery: true})
	if err != nil {
		t.Fatalf("idempotent Ensure on recovery secret: %v", err)
	}
	if calls != 0 {
		t.Fatalf("generator called %d times for existing recovery secret", calls)
	}

	// 7. GetMeta retrieves metadata from router map or fallback to FileStore.
	metaNew, err := r.GetMeta(ctx, newRef)
	if err != nil || metaNew.Owner != "teleport" {
		t.Fatalf("GetMeta(%q) = %+v, err=%v", newRef, metaNew, err)
	}
	metaRec, err := r.GetMeta(ctx, recRef)
	if err != nil || metaRec.Owner != "step-ca" || !metaRec.Recovery {
		t.Fatalf("GetMeta(%q) = %+v, err=%v", recRef, metaRec, err)
	}

	// 8. List merges Vault keys and recovery FileStore keys.
	entries, err := r.List(ctx, "")
	if err != nil {
		t.Fatalf("Router.List: %v", err)
	}
	// We expect preRef, newRef (from Vault) and recRef (from FileStore).
	found := map[Ref]bool{}
	for _, e := range entries {
		found[e.Ref] = true
	}
	if !found[newRef] || !found[preRef] || !found[recRef] {
		t.Fatalf("Router.List missing entries, got: %+v", entries)
	}
}
