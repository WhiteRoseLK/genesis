// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"context"
	"log/slog"
	"regexp"
)

// knownSecretPatterns detects known secret patterns that could leak into a log
// message without going through the Secret type (docs/06-secrets-state.md
// "Redaction"): Vault tokens and PEM blocks.
var knownSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\bhvs\.[A-Za-z0-9_-]+\b`),                                   // token Vault (service)
	regexp.MustCompile(`\bhvb\.[A-Za-z0-9_-]+\b`),                                   // token Vault (batch)
	regexp.MustCompile(`\bs\.[A-Za-z0-9]{20,}\b`),                                   // legacy Vault token format
	regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]+-----.*?-----END [A-Z0-9 ]+-----`), // PEM block
}

const redacted = "***"

func redactString(s string) string {
	for _, pattern := range knownSecretPatterns {
		s = pattern.ReplaceAllString(s, redacted)
	}
	return s
}

// RedactingHandler wraps a slog.Handler: it redacts values of type Secret
// (through slog.LogValuer, already covered by Secret.LogValue) and known
// secret patterns in the message and the text attributes, to also cover the
// cases where a sensitive value leaks outside the Secret type.
type RedactingHandler struct {
	next slog.Handler
}

// NewRedactingHandler builds a redacting handler wrapping next.
func NewRedactingHandler(next slog.Handler) *RedactingHandler {
	return &RedactingHandler{next: next}
}

func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *RedactingHandler) Handle(ctx context.Context, record slog.Record) error {
	redactedRecord := slog.NewRecord(record.Time, record.Level, redactString(record.Message), record.PC)
	record.Attrs(func(a slog.Attr) bool {
		redactedRecord.AddAttrs(redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, redactedRecord)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = redactAttr(a)
	}
	return &RedactingHandler{next: h.next.WithAttrs(redacted)}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{next: h.next.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve() // triggers slog.LogValuer (including Secret.LogValue)
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, redactString(a.Value.String()))
	case slog.KindGroup:
		group := a.Value.Group()
		redacted := make([]slog.Attr, len(group))
		for i, sub := range group {
			redacted[i] = redactAttr(sub)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(redacted...)}
	default:
		return a
	}
}
