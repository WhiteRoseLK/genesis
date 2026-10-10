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

func TestTargetFilesWithoutHostKeyDisablesStrictHostKeyChecking(t *testing.T) {
	files := targetFiles(&ansiblev1.Target{
		Host:          "10.0.0.5",
		Port:          22,
		User:          "genesis",
		SshPrivateKey: "key",
	})
	if _, ok := files["known_hosts"]; ok {
		t.Errorf("known_hosts present although ssh_host_key is empty: %v", files)
	}
	inventory := string(files["inventory.ini"])
	if !strings.Contains(inventory, "StrictHostKeyChecking=no") {
		t.Errorf("inventory does not disable host key checking when ssh_host_key is empty: %s", inventory)
	}
	if !strings.Contains(inventory, "UserKnownHostsFile=/dev/null") {
		t.Errorf("inventory does not set UserKnownHostsFile=/dev/null: %s", inventory)
	}
}

func TestTargetFilesWithHostKeyEnablesStrictHostKeyChecking(t *testing.T) {
	const hostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleHostKeyPublic123"
	files := targetFiles(&ansiblev1.Target{
		Host:          "10.0.0.5",
		Port:          22,
		User:          "genesis",
		SshPrivateKey: "key",
		SshHostKey:    hostKey,
	})

	knownHosts, ok := files["known_hosts"]
	if !ok {
		t.Fatalf("known_hosts missing from targetFiles: %v", files)
	}
	expectedEntry := "10.0.0.5 " + hostKey + "\n"
	if string(knownHosts) != expectedEntry {
		t.Errorf("known_hosts = %q, want %q", string(knownHosts), expectedEntry)
	}

	inventory := string(files["inventory.ini"])
	if !strings.Contains(inventory, "StrictHostKeyChecking=yes") {
		t.Errorf("inventory does not enable StrictHostKeyChecking=yes: %s", inventory)
	}
	if !strings.Contains(inventory, "UserKnownHostsFile=/work/known_hosts") {
		t.Errorf("inventory does not point UserKnownHostsFile to /work/known_hosts: %s", inventory)
	}
}

func TestTargetFilesWithHostKeyNonStandardPort(t *testing.T) {
	const hostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleHostKeyPublic123"
	files := targetFiles(&ansiblev1.Target{
		Host:          "10.0.0.5",
		Port:          2222,
		User:          "genesis",
		SshPrivateKey: "key",
		SshHostKey:    hostKey,
	})

	knownHosts, ok := files["known_hosts"]
	if !ok {
		t.Fatalf("known_hosts missing from targetFiles: %v", files)
	}
	expectedEntry := "[10.0.0.5]:2222 " + hostKey + "\n"
	if string(knownHosts) != expectedEntry {
		t.Errorf("known_hosts = %q, want %q", string(knownHosts), expectedEntry)
	}
}

func TestFormatKnownHosts(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int32
		key  string
		want string
	}{
		{
			name: "standard port 22 bare key",
			host: "10.0.0.5",
			port: 22,
			key:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI123",
			want: "10.0.0.5 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI123\n",
		},
		{
			name: "port 0 defaults to standard host pattern",
			host: "10.0.0.5",
			port: 0,
			key:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI123",
			want: "10.0.0.5 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI123\n",
		},
		{
			name: "non-standard port brackets pattern",
			host: "10.0.0.5",
			port: 3022,
			key:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI123",
			want: "[10.0.0.5]:3022 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI123\n",
		},
		{
			name: "already formatted with host pattern",
			host: "10.0.0.5",
			port: 22,
			key:  "10.0.0.5 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI123",
			want: "10.0.0.5 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI123\n",
		},
		{
			name: "multi-line with comments and whitespace",
			host: "10.0.0.5",
			port: 22,
			key:  "# host key comment\n\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI123\nssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC...\n",
			want: "10.0.0.5 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI123\n10.0.0.5 ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC...\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatKnownHosts(tt.host, tt.port, tt.key)
			if got != tt.want {
				t.Errorf("formatKnownHosts() = %q, want %q", got, tt.want)
			}
		})
	}
}
