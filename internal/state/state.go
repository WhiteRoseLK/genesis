// SPDX-License-Identifier: Apache-2.0

// Package state est seul propriétaire de l'état de l'outil
// (docs/06-secrets-etat.md) : écriture atomique, verrou, jamais de valeur
// secrète (uniquement des références).
//
// Le contenu prévu par le doc (VM, statut par module, fournisseur actif de
// chaque fonction, versions, endpoints, historique des passations) sera
// ajouté par les jalons qui le produisent (résolveur J4, modules J5+) : le
// ajouter maintenant serait spéculatif, aucun composant ne le remplit encore.
package state

// State est le contenu persisté dans state_dir/state.json.
type State struct {
	SchemaVersion int `json:"schema_version"`
	// SecretsBackend est le backend actif du store de secrets ("file" ou
	// "vault" après passation, docs/06-secrets-etat.md).
	SecretsBackend string `json:"secrets_backend"`
}

// currentSchemaVersion est incrémenté à chaque changement de forme de State.
const currentSchemaVersion = 1

// New construit un état initial pour un `genesis init` frais.
func New() *State {
	return &State{
		SchemaVersion:  currentSchemaVersion,
		SecretsBackend: "file",
	}
}
