// SPDX-License-Identifier: Apache-2.0

// Package secrets génère, stocke et distribue les secrets de l'outil
// (docs/06-secrets-state.md). L'utilisateur ne fournit que les identifiants
// de l'hyperviseur ; tout le reste est généré.
package secrets

import "log/slog"

// Secret porte une valeur sensible. String() et MarshalJSON() la masquent
// systématiquement (docs/06 "Redaction") ; seul ExposeSecret y accède,
// pour marquer explicitement dans le code chaque endroit qui manipule la
// valeur en clair.
type Secret struct {
	value string
}

// NewSecret enveloppe une valeur sensible.
func NewSecret(value string) Secret {
	return Secret{value: value}
}

// ExposeSecret retourne la valeur en clair. Nom volontairement explicite :
// à n'utiliser que là où la valeur doit réellement être manipulée (chiffrement,
// `secrets get`), jamais pour du logging ou un message d'erreur.
func (s Secret) ExposeSecret() string {
	return s.value
}

func (s Secret) String() string {
	return "***"
}

func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"***"`), nil
}

// LogValue redacte la valeur pour slog.LogValuer (Go 1.21+) : tout attribut
// slog portant un Secret est automatiquement masqué, même sans passer par
// le handler de redaction de redact.go.
func (s Secret) LogValue() slog.Value {
	return slog.StringValue("***")
}
