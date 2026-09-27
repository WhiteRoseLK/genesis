// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"time"
)

// Meta describes a secret without ever carrying its value
// (docs/06-secrets-state.md).
type Meta struct {
	Owner     string    `json:"owner"`     // owning capability
	Consumers []string  `json:"consumers"` // consuming capabilities / VMs
	Kind      string    `json:"kind"`      // password | token | private-key | certificate | ssh-key
	CreatedAt time.Time `json:"created_at"`
	Rotation  string    `json:"rotation"` // never | on-handover | 90d …
	Recovery  bool      `json:"recovery"` // must remain as a local copy after the handover
}

// Entry is the result of List: metadata only, never a value.
type Entry struct {
	Ref  Ref
	Meta Meta
}

// Generator produces a secret's initial value during an Ensure.
type Generator func() (Secret, error)

// Store is the secret storage interface (docs/06-secrets-state.md). Planned
// implementations: file (iteration 1, seed) and vault (target).
type Store interface {
	// Ensure creates secret ref with gen if it is missing; does nothing if it
	// already exists (idempotent, M2 acceptance criterion).
	Ensure(ctx context.Context, ref Ref, gen Generator, meta Meta) error
	Get(ctx context.Context, ref Ref) (Secret, error)
	// GetMeta reads a secret's metadata without its value — used for access
	// control (owner/consumers) before serving Get to a module through the
	// core.secrets/v1 function (internal/broker).
	GetMeta(ctx context.Context, ref Ref) (Meta, error)
	Put(ctx context.Context, ref Ref, s Secret, meta Meta) error
	List(ctx context.Context, prefix string) ([]Entry, error)
	Backend() string
}
