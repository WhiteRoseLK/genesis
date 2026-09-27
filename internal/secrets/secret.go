// SPDX-License-Identifier: Apache-2.0

// Package secrets generates, stores and distributes the tool's secrets
// (docs/06-secrets-state.md). The user provides only the hypervisor
// credentials; everything else is generated.
package secrets

import "log/slog"

// Secret carries a sensitive value. String() and MarshalJSON() always mask it
// (docs/06 "Redaction"); only ExposeSecret accesses it, to explicitly mark in
// the code every place that handles the plaintext value.
type Secret struct {
	value string
}

// NewSecret wraps a sensitive value.
func NewSecret(value string) Secret {
	return Secret{value: value}
}

// ExposeSecret returns the plaintext value. The name is deliberately explicit:
// use it only where the value really has to be handled (encryption, `secrets
// get`), never for logging or an error message.
func (s Secret) ExposeSecret() string {
	return s.value
}

func (s Secret) String() string {
	return "***"
}

func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"***"`), nil
}

// LogValue redacts the value for slog.LogValuer (Go 1.21+): any slog attribute
// carrying a Secret is masked automatically, even without going through the
// redacting handler of redact.go.
func (s Secret) LogValue() slog.Value {
	return slog.StringValue("***")
}
