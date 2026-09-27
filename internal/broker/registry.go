// SPDX-License-Identifier: Apache-2.0

// Package broker routes function calls between modules
// (docs/02-architecture.md): a function → active provider registry, and access
// control — a module can only call the functions declared in its resolved
// requires (a broker session never registers anything else).
package broker

import (
	"sync"

	"google.golang.org/grpc"
)

// nativeFactory builds, for a given caller, the registrar of a function
// provided natively by the core (e.g. core.secrets/v1) — the caller's identity
// is used for access control (docs/02).
type nativeFactory func(caller string) func(*grpc.Server)

// Registry knows, for each function, its active provider: native (core) or
// module (currently dispensed connection).
type Registry struct {
	mu      sync.RWMutex
	native  map[string]nativeFactory
	forward map[string]func(*grpc.Server) // functions provided by a module
	// fleet: "fleet" functions (docs/09-decisions.md ADR-017) — every
	// accumulated connection is called (fan-out), never a single active
	// provider replacing the previous one as in forward.
	fleet map[string][]*grpc.ClientConn
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		native:  map[string]nativeFactory{},
		forward: map[string]func(*grpc.Server){},
		fleet:   map[string][]*grpc.ClientConn{},
	}
}

// SetNative registers a function provided natively by the core.
func (r *Registry) SetNative(function string, factory nativeFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.native[function] = factory
}

// SetModuleProvider registers/updates the active provider of a function
// provided by a module: conn is the module's dispensed connection
// (internal/modulehost.Client.DispenseFunction), register knows how to build a
// service that relays each call to conn (see forward_*.go).
func (r *Registry) SetModuleProvider(function string, conn *grpc.ClientConn, register func(*grpc.Server, *grpc.ClientConn)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forward[function] = func(s *grpc.Server) { register(s, conn) }
}

// AddFleetProvider accumulates an extra connection for a "fleet" function
// (ADR-017) — unlike SetModuleProvider, which replaces the single active
// provider, several providers coexist for the same fleet function, all of them
// called (see ForwardFleetAgent, fan-out).
func (r *Registry) AddFleetProvider(function string, conn *grpc.ClientConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fleet[function] = append(r.fleet[function], conn)
}

// Unset removes a function from the registry (e.g. SeedDown of the seed
// provider after the handover).
func (r *Registry) Unset(function string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.native, function)
	delete(r.forward, function)
}

// HasProvider reports whether function has an active provider.
func (r *Registry) HasProvider(function string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, native := r.native[function]
	_, forwarded := r.forward[function]
	return native || forwarded || len(r.fleet[function]) > 0
}

// BuildSession builds a grpc.Server that only exposes the functions listed in
// allowed (the caller's resolved requires) — this is the access-control
// mechanism itself: an unlisted function is never registered, and calling it
// fails with codes.Unimplemented (M4 acceptance criterion, doc 08: "a call to
// an undeclared function is refused by the broker").
func (r *Registry) BuildSession(caller string, allowed []string, opts ...grpc.ServerOption) *grpc.Server {
	s := grpc.NewServer(opts...)

	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, fn := range allowed {
		if factory, ok := r.native[fn]; ok {
			factory(caller)(s)
			continue
		}
		if conns, ok := r.fleet[fn]; ok && len(conns) > 0 {
			if registerFleet, ok := fleetForwarderFor(fn); ok {
				registerFleet(s, conns)
			}
			continue
		}
		if register, ok := r.forward[fn]; ok {
			register(s)
			continue
		}
		// Declared but without an active provider: nothing is registered, and
		// a call will fail like any undeclared function. The resolver
		// (internal/resolver) is supposed to have refused this case earlier,
		// except for a missing optional function.
	}
	return s
}
