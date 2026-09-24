// SPDX-License-Identifier: Apache-2.0

// Package state est seul propriétaire de l'état de l'outil
// (docs/06-secrets-etat.md) : écriture atomique, verrou, jamais de valeur
// secrète (uniquement des références).
//
// VM, versions, endpoints et historique des passations restent différés aux
// jalons qui les produisent (modules réels, J5+) : les ajouter maintenant
// serait spéculatif, aucun composant ne les remplit encore.
package state

// State est le contenu persisté dans state_dir/state.json.
type State struct {
	SchemaVersion int `json:"schema_version"`
	// SecretsBackend est le backend actif du store de secrets ("file" ou
	// "vault" après passation, docs/06-secrets-etat.md).
	SecretsBackend string `json:"secrets_backend"`
	// SeedRetired passe à vrai quand tous les services graine ont été
	// arrêtés en fin d'apply (docs/05-cycle-bootstrap.md, phase 4) : un apply
	// ultérieur ne relance plus jamais la graine.
	SeedRetired bool `json:"seed_retired,omitempty"`
	// Modules porte, par module, l'état opaque renvoyé par sa dernière étape
	// réussie (StepResult.state, docs/03-contrat-module.md §2) — c'est ce
	// qui permet à `Check` de constater qu'une étape est déjà conforme
	// (internal/engine, jalon J4).
	Modules map[string]ModuleState `json:"modules,omitempty"`
}

// ModuleState est l'état propre à un module, opaque pour le cœur.
type ModuleState struct {
	// StateJSON est le dernier StepResult.state reçu, en JSON, à repasser
	// tel quel au module lors du prochain appel.
	StateJSON []byte `json:"state_json,omitempty"`
}

// currentSchemaVersion est incrémenté à chaque changement de forme de State.
const currentSchemaVersion = 1

// New construit un état initial pour un `genesis init` frais.
func New() *State {
	return &State{
		SchemaVersion:  currentSchemaVersion,
		SecretsBackend: "file",
		Modules:        map[string]ModuleState{},
	}
}
