// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
)

func TestGeneratePassword(t *testing.T) {
	s, err := GeneratePassword()()
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	value := s.ExposeSecret()
	if len(value) != 32 {
		t.Errorf("length = %d, want 32", len(value))
	}
	for _, r := range value {
		if !strings.ContainsRune(passwordAlphabet, r) {
			t.Errorf("character %q outside the safe alphabet", r)
		}
	}
}

func TestGenerateTokenIsUniqueAnd256Bits(t *testing.T) {
	a, err := GenerateToken()()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	b, err := GenerateToken()()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if a.ExposeSecret() == b.ExposeSecret() {
		t.Error("two generated tokens are identical")
	}
	if len(a.ExposeSecret()) != 64 { // 32 bytes in hex
		t.Errorf("length = %d, want 64 (256 bits in hex)", len(a.ExposeSecret()))
	}
}

func TestGenerateECDSAAndEd25519Keys(t *testing.T) {
	for name, gen := range map[string]Generator{
		"ecdsa-p384": GenerateECDSAP384Key(),
		"ed25519":    GenerateEd25519Key(),
	} {
		t.Run(name, func(t *testing.T) {
			s, err := gen()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			block, _ := pem.Decode([]byte(s.ExposeSecret()))
			if block == nil || block.Type != "PRIVATE KEY" {
				t.Errorf("%s: invalid PEM or unexpected type: %+v", name, block)
			}
		})
	}
}

func TestGenerateSSHKeyPair(t *testing.T) {
	s, err := GenerateSSHKeyPair()()
	if err != nil {
		t.Fatalf("GenerateSSHKeyPair: %v", err)
	}
	var pair SSHKeyPair
	if err := json.Unmarshal([]byte(s.ExposeSecret()), &pair); err != nil {
		t.Fatalf("decoding the SSH pair: %v", err)
	}
	if !strings.HasPrefix(pair.PublicKeyAuthorized, "ssh-ed25519 ") {
		t.Errorf("unexpected public key: %q", pair.PublicKeyAuthorized)
	}
	if !strings.Contains(pair.PrivateKeyOpenSSH, "PRIVATE KEY-----") {
		t.Errorf("unexpected OpenSSH private key: %q", pair.PrivateKeyOpenSSH)
	}
}
