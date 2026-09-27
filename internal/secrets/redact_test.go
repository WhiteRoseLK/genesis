// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// TestRedaction is the test explicitly required by the M2 acceptance criterion
// (doc 08): nothing sensitive may appear in the logger output, whether it is
// an attribute of type Secret or a known secret pattern slipped into an
// ordinary string.
func TestRedaction(t *testing.T) {
	const (
		vaultToken = "hvs.CAESIJ-example-not-a-real-token-1234567890"
		pemKey     = "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEA\n-----END PRIVATE KEY-----"
	)
	secretValue := NewSecret("plaintext-password")

	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, nil)
	logger := slog.New(NewRedactingHandler(base))

	logger.Info("bootstrap",
		"password", secretValue,
		"vault_token_in_message", vaultToken,
		"pem_block", pemKey,
	)
	logger.With("nested_token", vaultToken).Info("handover")
	logger.Info("message with an embedded token: "+vaultToken, "k", "v")

	output := buf.String()

	for _, leaked := range []string{"plaintext-password", vaultToken, pemKey, "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEA"} {
		if strings.Contains(output, leaked) {
			t.Errorf("the logger output contains a sensitive value %q\n--- output ---\n%s", leaked, output)
		}
	}
	if !strings.Contains(output, redacted) {
		t.Errorf("the logger output never contains %q: redaction probably did not happen\n--- output ---\n%s", redacted, output)
	}
}

func TestSecretStringAndJSONAreRedacted(t *testing.T) {
	s := NewSecret("sensitive-value")
	if s.String() != redacted {
		t.Errorf("String() = %q, want %q", s.String(), redacted)
	}
	b, err := s.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != `"***"` {
		t.Errorf("MarshalJSON() = %s, want %q", b, `"***"`)
	}
	if strings.Contains(string(b), "sensitive-value") {
		t.Errorf("MarshalJSON() leaked the value: %s", b)
	}
}

func TestRedactionHandlerRespectsContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewRedactingHandler(slog.NewJSONHandler(&buf, nil)))
	logger.InfoContext(context.Background(), "ok", "password", NewSecret("x"))
	if strings.Contains(buf.String(), `"x"`) {
		t.Errorf("the secret value leaked: %s", buf.String())
	}
}
