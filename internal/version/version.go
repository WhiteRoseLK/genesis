// SPDX-License-Identifier: Apache-2.0

// Package version porte la version du cœur, comparée à la contrainte `core`
// du manifest de chaque module (docs/03-contrat-module.md).
package version

// Version est mise à jour par Release Please dans la PR de release (marqueur
// ci-dessous) : le binaire publié, le tag et la vérification de
// compatibilité des modules parlent donc toujours de la même version.
const Version = "0.1.0" // x-release-please-version
