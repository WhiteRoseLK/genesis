// SPDX-License-Identifier: Apache-2.0

// Package integration teste chaque module du dépôt à travers le vrai hôte
// de modules du cœur (internal/modulehost, internal/broker), fonctions
// requises simulées (docs/03-contrat-module.md, règle 7). Les tests qui
// pilotent de vrais conteneurs portent le build tag `docker` :
// `make test-docker`.
package integration
