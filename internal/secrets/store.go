// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"time"
)

// Meta décrit un secret sans jamais porter sa valeur (docs/06-secrets-etat.md).
type Meta struct {
	Owner     string    `json:"owner"`     // capacité propriétaire
	Consumers []string  `json:"consumers"` // capacités / VM consommatrices
	Kind      string    `json:"kind"`      // password | token | private-key | certificate | ssh-key
	CreatedAt time.Time `json:"created_at"`
	Rotation  string    `json:"rotation"` // never | on-handover | 90d …
	Recovery  bool      `json:"recovery"` // doit rester en copie locale après passation
}

// Entry est le résultat de List : métadonnées seulement, jamais de valeur.
type Entry struct {
	Ref  Ref
	Meta Meta
}

// Generator produit la valeur initiale d'un secret lors d'un Ensure.
type Generator func() (Secret, error)

// Store est l'interface de stockage des secrets (docs/06-secrets-etat.md).
// Implémentations prévues : file (itération 1, graine) et vault (cible).
type Store interface {
	// Ensure crée le secret ref via gen s'il est absent ; ne fait rien s'il
	// existe déjà (idempotent, critère d'acceptation J2).
	Ensure(ctx context.Context, ref Ref, gen Generator, meta Meta) error
	Get(ctx context.Context, ref Ref) (Secret, error)
	Put(ctx context.Context, ref Ref, s Secret, meta Meta) error
	List(ctx context.Context, prefix string) ([]Entry, error)
	Backend() string
}
