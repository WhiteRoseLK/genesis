// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// TestRedaction est le test explicitement requis par le critère
// d'acceptation du jalon J2 (doc 08) : rien de sensible ne doit apparaître
// dans la sortie du logger, qu'il s'agisse d'un attribut de type Secret ou
// d'un motif de secret connu glissé dans une chaîne ordinaire.
func TestRedaction(t *testing.T) {
	const (
		vaultToken = "hvs.CAESIJ-example-not-a-real-token-1234567890"
		pemKey     = "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEA\n-----END PRIVATE KEY-----"
	)
	secretValue := NewSecret("mot-de-passe-en-clair")

	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, nil)
	logger := slog.New(NewRedactingHandler(base))

	logger.Info("bootstrap",
		"password", secretValue,
		"vault_token_in_message", vaultToken,
		"pem_block", pemKey,
	)
	logger.With("nested_token", vaultToken).Info("passation")
	logger.Info("message avec token intégré : "+vaultToken, "k", "v")

	output := buf.String()

	for _, leaked := range []string{"mot-de-passe-en-clair", vaultToken, pemKey, "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEA"} {
		if strings.Contains(output, leaked) {
			t.Errorf("la sortie du logger contient une valeur sensible %q\n--- sortie ---\n%s", leaked, output)
		}
	}
	if !strings.Contains(output, redacted) {
		t.Errorf("la sortie du logger ne contient jamais %q : la redaction n'a probablement pas eu lieu\n--- sortie ---\n%s", redacted, output)
	}
}

func TestSecretStringAndJSONAreRedacted(t *testing.T) {
	s := NewSecret("valeur-sensible")
	if s.String() != redacted {
		t.Errorf("String() = %q, attendu %q", s.String(), redacted)
	}
	b, err := s.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON : %v", err)
	}
	if string(b) != `"***"` {
		t.Errorf("MarshalJSON() = %s, attendu %q", b, `"***"`)
	}
	if strings.Contains(string(b), "valeur-sensible") {
		t.Errorf("MarshalJSON() a laissé fuiter la valeur : %s", b)
	}
}

func TestRedactionHandlerRespectsContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewRedactingHandler(slog.NewJSONHandler(&buf, nil)))
	logger.InfoContext(context.Background(), "ok", "password", NewSecret("x"))
	if strings.Contains(buf.String(), `"x"`) {
		t.Errorf("la valeur du secret a fuité : %s", buf.String())
	}
}
