// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"

	"golang.org/x/crypto/ssh"
)

// Alphabet without ambiguous characters (0/O, 1/l/I) for generated passwords.
const passwordAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"

// GeneratePassword produces a 32-character password from a safe alphabet
// (docs/06-secrets-state.md).
func GeneratePassword() Generator {
	return func() (Secret, error) {
		const length = 32
		buf := make([]byte, length)
		alphabetLen := big.NewInt(int64(len(passwordAlphabet)))
		for i := range buf {
			n, err := rand.Int(rand.Reader, alphabetLen)
			if err != nil {
				return Secret{}, fmt.Errorf("generating the password: %w", err)
			}
			buf[i] = passwordAlphabet[n.Int64()]
		}
		return NewSecret(string(buf)), nil
	}
}

// GenerateToken produces a random 256-bit token, hex-encoded.
func GenerateToken() Generator {
	return func() (Secret, error) {
		buf := make([]byte, 32) // 256 bits
		if _, err := rand.Read(buf); err != nil {
			return Secret{}, fmt.Errorf("generating the token: %w", err)
		}
		return NewSecret(hex.EncodeToString(buf)), nil
	}
}

// GenerateECDSAP384Key produces an ECDSA P-384 private key as PKCS8 PEM.
func GenerateECDSAP384Key() Generator {
	return func() (Secret, error) {
		key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			return Secret{}, fmt.Errorf("generating the ECDSA P-384 key: %w", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return Secret{}, fmt.Errorf("encoding the ECDSA P-384 key: %w", err)
		}
		return NewSecret(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))), nil
	}
}

// GenerateEd25519Key produces an Ed25519 private key as PKCS8 PEM.
func GenerateEd25519Key() Generator {
	return func() (Secret, error) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return Secret{}, fmt.Errorf("generating the Ed25519 key: %w", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			return Secret{}, fmt.Errorf("encoding the Ed25519 key: %w", err)
		}
		return NewSecret(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))), nil
	}
}

// SSHKeyPair is the JSON value carried by the Secret produced by
// GenerateSSHKeyPair: the OpenSSH private key and the public authorized_keys
// line.
type SSHKeyPair struct {
	PrivateKeyOpenSSH   string `json:"private_key_openssh"`
	PublicKeyAuthorized string `json:"public_key_authorized"`
}

// GenerateSSHKeyPair produces an Ed25519 SSH key pair
// (docs/06-secrets-state.md).
func GenerateSSHKeyPair() Generator {
	return func() (Secret, error) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return Secret{}, fmt.Errorf("generating the Ed25519 SSH pair: %w", err)
		}
		block, err := ssh.MarshalPrivateKey(priv, "")
		if err != nil {
			return Secret{}, fmt.Errorf("encoding the SSH private key: %w", err)
		}
		sshPub, err := ssh.NewPublicKey(pub)
		if err != nil {
			return Secret{}, fmt.Errorf("encoding the SSH public key: %w", err)
		}
		pair := SSHKeyPair{
			PrivateKeyOpenSSH:   string(pem.EncodeToMemory(block)),
			PublicKeyAuthorized: string(ssh.MarshalAuthorizedKey(sshPub)),
		}
		value, err := json.Marshal(pair)
		if err != nil {
			return Secret{}, fmt.Errorf("encoding the SSH pair: %w", err)
		}
		return NewSecret(string(value)), nil
	}
}
