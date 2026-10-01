// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	secretskvv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/secrets/kv/v1"
)

// Router implements Store (docs/06-secrets-state.md): it initially routes all
// calls to FileStore (seed phase, backend "file"). When secrets migrate to
// vault (target phase, backend "vault"), SwitchToVault directs all subsequent
// writes and reads to secrets.kv/v1 while switching FileStore to read-only mode
// as an encrypted backup copy for recovery secrets (docs/05-bootstrap-lifecycle.md).
type Router struct {
	mu        sync.RWMutex
	backend   string
	fileStore *FileStore
	kvClient  secretskvv1.SecretsKVClient
	metas     map[Ref]Meta
}

var _ Store = (*Router)(nil)

// NewRouter builds a Router wrapping the initial file store.
func NewRouter(fileStore *FileStore) *Router {
	return &Router{
		backend:   "file",
		fileStore: fileStore,
		metas:     make(map[Ref]Meta),
	}
}

// FileStore returns the underlying FileStore.
func (r *Router) FileStore() *FileStore {
	return r.fileStore
}

// SwitchToVault routes future operations to Vault through the provided
// secrets.kv/v1 client and marks the local FileStore as read-only.
func (r *Router) SwitchToVault(kv secretskvv1.SecretsKVClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backend = "vault"
	r.kvClient = kv
	if r.fileStore != nil {
		r.fileStore.SetReadOnly(true)
	}
}

// Backend returns the currently active secret backend ("file" or "vault").
func (r *Router) Backend() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.backend
}

// Ensure creates secret ref with gen if it is missing; does nothing if it
// already exists (idempotent, M2 acceptance criterion).
func (r *Router) Ensure(ctx context.Context, ref Ref, gen Generator, meta Meta) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := ref.Validate(); err != nil {
		return err
	}

	if r.backend == "file" {
		return r.fileStore.Ensure(ctx, ref, gen, meta)
	}

	if r.kvClient == nil {
		return errors.New("secrets backend is vault but kv client is not connected")
	}

	// 1. Check if already present in Vault (idempotent).
	resp, err := r.kvClient.Read(ctx, &secretskvv1.ReadRequest{Ref: string(ref)})
	if err == nil && resp.GetFound() {
		r.metas[ref] = meta
		return nil
	}

	// 2. Check if already present in FileStore (e.g. recovery secrets kept in file).
	if _, err := r.fileStore.Get(ctx, ref); err == nil {
		r.metas[ref] = meta
		return nil
	}

	// 3. Generate and write to Vault.
	val, err := gen()
	if err != nil {
		return fmt.Errorf("generating secret %s: %w", ref, err)
	}

	if _, err := r.kvClient.Write(ctx, &secretskvv1.WriteRequest{
		Ref:   string(ref),
		Value: val.ExposeSecret(),
	}); err != nil {
		return fmt.Errorf("writing secret %s to vault: %w", ref, err)
	}

	r.metas[ref] = meta
	return nil
}

// Get reveals the secret value: reads from Vault if backend is vault (falling
// back to FileStore for recovery secrets), or directly from FileStore.
func (r *Router) Get(ctx context.Context, ref Ref) (Secret, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if err := ref.Validate(); err != nil {
		return Secret{}, err
	}

	if r.backend == "file" {
		return r.fileStore.Get(ctx, ref)
	}

	if r.kvClient != nil {
		resp, err := r.kvClient.Read(ctx, &secretskvv1.ReadRequest{Ref: string(ref)})
		if err == nil && resp.GetFound() {
			return NewSecret(resp.GetValue()), nil
		}
	}

	// Fall back to FileStore for recovery secrets (e.g. root CA, Vault unseal keys).
	return r.fileStore.Get(ctx, ref)
}

// GetMeta reads a secret's metadata without its value — used for access control.
func (r *Router) GetMeta(ctx context.Context, ref Ref) (Meta, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if err := ref.Validate(); err != nil {
		return Meta{}, err
	}

	if meta, ok := r.metas[ref]; ok {
		return meta, nil
	}

	return r.fileStore.GetMeta(ctx, ref)
}

// Put writes a secret value and its metadata.
func (r *Router) Put(ctx context.Context, ref Ref, s Secret, meta Meta) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := ref.Validate(); err != nil {
		return err
	}

	if r.backend == "file" {
		return r.fileStore.Put(ctx, ref, s, meta)
	}

	if r.kvClient == nil {
		return errors.New("secrets backend is vault but kv client is not connected")
	}

	if _, err := r.kvClient.Write(ctx, &secretskvv1.WriteRequest{
		Ref:   string(ref),
		Value: s.ExposeSecret(),
	}); err != nil {
		return fmt.Errorf("writing secret %s to vault: %w", ref, err)
	}

	r.metas[ref] = meta
	return nil
}

// List returns metadata for all secrets matching prefix.
func (r *Router) List(ctx context.Context, prefix string) ([]Entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.backend == "file" {
		return r.fileStore.List(ctx, prefix)
	}

	seen := map[Ref]bool{}
	var entries []Entry

	if r.kvClient != nil {
		resp, err := r.kvClient.List(ctx, &secretskvv1.ListRequest{Prefix: prefix})
		if err != nil {
			return nil, fmt.Errorf("listing secrets from vault: %w", err)
		}
		for _, refStr := range resp.GetRefs() {
			ref := Ref(refStr)
			seen[ref] = true
			meta := r.metas[ref]
			if meta.Owner == "" {
				if fileMeta, err := r.fileStore.GetMeta(ctx, ref); err == nil {
					meta = fileMeta
				}
			}
			entries = append(entries, Entry{Ref: ref, Meta: meta})
		}
	}

	fileEntries, err := r.fileStore.List(ctx, prefix)
	if err == nil {
		for _, fe := range fileEntries {
			if !seen[fe.Ref] && fe.Meta.Recovery {
				seen[fe.Ref] = true
				entries = append(entries, fe)
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Ref < entries[j].Ref
	})
	return entries, nil
}
