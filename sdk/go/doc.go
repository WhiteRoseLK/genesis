// SPDX-License-Identifier: Apache-2.0

// Package sdk est la seule dépendance autorisée pour un module
// (docs/02-architecture.md, docs/03-contrat-module.md) : le protocole
// module/v1 généré (sdk/go/gen), Serve pour l'exposer en plugin go-plugin,
// et le chargement du manifest module.yaml.
package sdk
