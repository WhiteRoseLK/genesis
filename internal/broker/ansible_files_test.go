// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"archive/tar"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestTarFilesArePrivate(t *testing.T) {
	archive, err := tarFiles(map[string][]byte{
		"id_target": []byte("clé"),
		"vars.json": []byte(`{"a":"b"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(archive))
	seen := map[string]string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Mode != 0o600 {
			t.Errorf("%s : mode %o, attendu 0600", h.Name, h.Mode)
		}
		content, _ := io.ReadAll(tr)
		seen[h.Name] = string(content)
	}
	if seen["id_target"] != "clé" || seen["vars.json"] != `{"a":"b"}` {
		t.Errorf("contenu inattendu : %v", seen)
	}
}

func TestRedactValuesMasksSecretsFromVars(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASC\n-----END PRIVATE KEY-----"
	vars := map[string]any{
		"secret_id": "4f1c2e9a-secret-value",
		"nested":    map[string]any{"tls_key": pem},
		"list":      []any{"s3cr3t-in-a-list"},
		"port":      "8200",
	}
	out := "fatal: secret_id=4f1c2e9a-secret-value key=MIIEvQIBADANBgkqhkiG9w0BAQEFAASC item=s3cr3t-in-a-list port=8200"

	got := redactValues(out, collectStrings(vars))

	for _, leaked := range []string{"4f1c2e9a-secret-value", "MIIEvQIBADANBgkqhkiG9w0BAQEFAASC", "s3cr3t-in-a-list"} {
		if strings.Contains(got, leaked) {
			t.Errorf("valeur secrète %q encore présente : %s", leaked, got)
		}
	}
	if !strings.Contains(got, "port=8200") {
		t.Errorf("valeur courte masquée à tort : %s", got)
	}
}
