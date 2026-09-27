// SPDX-License-Identifier: Apache-2.0

// Package state is the sole owner of the tool's state
// (docs/06-secrets-state.md): atomic writes, lock, never a secret value (only
// references).
//
// VMs, versions, endpoints and handover history remain deferred to the
// milestones that produce them (real modules, M5+): adding them now would be
// speculative, no component fills them yet.
package state

// State is the content persisted in state_dir/state.json.
type State struct {
	SchemaVersion int `json:"schema_version"`
	// SecretsBackend is the active backend of the secret store ("file", or
	// "vault" after the handover, docs/06-secrets-state.md).
	SecretsBackend string `json:"secrets_backend"`
	// SeedRetired becomes true when every seed service has been stopped at the
	// end of apply (docs/05-bootstrap-lifecycle.md, phase 4): a later apply
	// never starts the seed again.
	SeedRetired bool `json:"seed_retired,omitempty"`
	// Modules holds, per module, the opaque state returned by its last
	// successful step (StepResult.state, docs/03-module-contract.md §2) — this
	// is what lets `Check` find that a step is already compliant
	// (internal/engine, milestone M4).
	Modules map[string]ModuleState `json:"modules,omitempty"`
}

// ModuleState is a module's own state, opaque to the core.
type ModuleState struct {
	// StateJSON is the last StepResult.state received, as JSON, to be passed
	// back as is to the module on the next call.
	StateJSON []byte `json:"state_json,omitempty"`
}

// currentSchemaVersion is incremented on every change to the shape of State.
const currentSchemaVersion = 1

// New builds an initial state for a fresh `genesis init`.
func New() *State {
	return &State{
		SchemaVersion:  currentSchemaVersion,
		SecretsBackend: "file",
		Modules:        map[string]ModuleState{},
	}
}
