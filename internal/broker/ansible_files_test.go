// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"archive/tar"
	"bytes"
	"io"
	"strings"
	"testing"

	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
)

func TestTarFilesArePrivate(t *testing.T) {
	archive, err := tarFiles(map[string][]byte{
		"id_target": []byte("key"),
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
			t.Errorf("%s: mode %o, want 0600", h.Name, h.Mode)
		}
		content, _ := io.ReadAll(tr)
		seen[h.Name] = string(content)
	}
	if seen["id_target"] != "key" || seen["vars.json"] != `{"a":"b"}` {
		t.Errorf("unexpected content: %v", seen)
	}
}

func TestTargetFilesOmitsCertificateByDefault(t *testing.T) {
	files := targetFiles(&ansiblev1.Target{Host: "10.0.0.5", Port: 22, User: "genesis", SshPrivateKey: "key"})
	if _, ok := files["id_target-cert.pub"]; ok {
		t.Errorf("id_target-cert.pub written although ssh_certificate_pem is empty (ADR-018): %v", files)
	}
	if string(files["id_target"]) != "key" {
		t.Errorf("id_target = %q, want %q", files["id_target"], "key")
	}
}

func TestTargetFilesWritesCertificateNextToTheKey(t *testing.T) {
	const cert = "ssh-ed25519-cert-v01@openssh.com AAAA..."
	files := targetFiles(&ansiblev1.Target{
		Host: "10.0.0.5", Port: 3022, User: "genesis",
		SshPrivateKey: "key", SshCertificatePem: cert,
	})
	if string(files["id_target-cert.pub"]) != cert {
		t.Errorf("id_target-cert.pub = %q, want %q (ADR-018: OpenSSH looks up a certificate next to its private key by this exact name)", files["id_target-cert.pub"], cert)
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
			t.Errorf("secret value %q still present: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "port=8200") {
		t.Errorf("short value wrongly masked: %s", got)
	}
}
