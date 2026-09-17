// SPDX-License-Identifier: Apache-2.0

// Package broker route les appels de fonction entre modules
// (docs/02-architecture.md) : registre fonction → fournisseur actif, et
// contrôle d'accès — un module ne peut appeler que les fonctions déclarées
// dans son requires résolu (une session de broker n'enregistre jamais
// autre chose).
package broker

import (
	"sync"

	"google.golang.org/grpc"
)

// nativeFactory construit, pour un appelant donné, le registrar d'une
// fonction fournie nativement par le cœur (ex. core.secrets/v1) — l'identité
// de l'appelant sert au contrôle d'accès (docs/02).
type nativeFactory func(caller string) func(*grpc.Server)

// Registry connaît, pour chaque fonction, son fournisseur actif : natif
// (core) ou module (connexion dispensée en cours).
type Registry struct {
	mu      sync.RWMutex
	native  map[string]nativeFactory
	forward map[string]func(*grpc.Server) // fonctions fournies par un module
}

// NewRegistry construit un registre vide.
func NewRegistry() *Registry {
	return &Registry{
		native:  map[string]nativeFactory{},
		forward: map[string]func(*grpc.Server){},
	}
}

// SetNative enregistre une fonction fournie nativement par le cœur.
func (r *Registry) SetNative(function string, factory nativeFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.native[function] = factory
}

// SetModuleProvider enregistre/actualise le fournisseur actif d'une fonction
// fournie par un module : conn est la connexion dispensée du module
// (internal/modulehost.Client.DispenseFunction), register sait construire un
// service qui relaie chaque appel vers conn (voir forward_*.go).
func (r *Registry) SetModuleProvider(function string, conn *grpc.ClientConn, register func(*grpc.Server, *grpc.ClientConn)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forward[function] = func(s *grpc.Server) { register(s, conn) }
}

// Unset retire une fonction du registre (ex. SeedDown du fournisseur graine
// après passation).
func (r *Registry) Unset(function string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.native, function)
	delete(r.forward, function)
}

// HasProvider indique si function a un fournisseur actif.
func (r *Registry) HasProvider(function string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, native := r.native[function]
	_, forwarded := r.forward[function]
	return native || forwarded
}

// BuildSession construit un grpc.Server n'exposant que les fonctions listées
// dans allowed (le requires résolu de l'appelant) — c'est le mécanisme même
// du contrôle d'accès : une fonction non listée n'est jamais enregistrée,
// l'appeler échoue avec codes.Unimplemented (critère d'acceptation J4,
// doc 08 : "appel d'une fonction non déclarée refusé par le broker").
func (r *Registry) BuildSession(caller string, allowed []string, opts ...grpc.ServerOption) *grpc.Server {
	s := grpc.NewServer(opts...)

	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, fn := range allowed {
		if factory, ok := r.native[fn]; ok {
			factory(caller)(s)
			continue
		}
		if register, ok := r.forward[fn]; ok {
			register(s)
			continue
		}
		// Déclarée mais sans fournisseur actif : rien n'est enregistré, un
		// appel échouera comme n'importe quelle fonction non déclarée. Le
		// résolveur (internal/resolver) est censé avoir déjà refusé ce cas
		// plus tôt, sauf fonction optionnelle absente.
	}
	return s
}
