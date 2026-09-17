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

// Alphabet sans caractères ambigus (0/O, 1/l/I) pour les mots de passe générés.
const passwordAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"

// GeneratePassword produit un mot de passe de 32 caractères sur un alphabet
// sûr (docs/06-secrets-etat.md).
func GeneratePassword() Generator {
	return func() (Secret, error) {
		const length = 32
		buf := make([]byte, length)
		alphabetLen := big.NewInt(int64(len(passwordAlphabet)))
		for i := range buf {
			n, err := rand.Int(rand.Reader, alphabetLen)
			if err != nil {
				return Secret{}, fmt.Errorf("génération du mot de passe : %w", err)
			}
			buf[i] = passwordAlphabet[n.Int64()]
		}
		return NewSecret(string(buf)), nil
	}
}

// GenerateToken produit un token aléatoire de 256 bits, encodé en hexadécimal.
func GenerateToken() Generator {
	return func() (Secret, error) {
		buf := make([]byte, 32) // 256 bits
		if _, err := rand.Read(buf); err != nil {
			return Secret{}, fmt.Errorf("génération du token : %w", err)
		}
		return NewSecret(hex.EncodeToString(buf)), nil
	}
}

// GenerateECDSAP384Key produit une clé privée ECDSA P-384 au format PEM PKCS8.
func GenerateECDSAP384Key() Generator {
	return func() (Secret, error) {
		key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			return Secret{}, fmt.Errorf("génération de la clé ECDSA P-384 : %w", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return Secret{}, fmt.Errorf("encodage de la clé ECDSA P-384 : %w", err)
		}
		return NewSecret(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))), nil
	}
}

// GenerateEd25519Key produit une clé privée Ed25519 au format PEM PKCS8.
func GenerateEd25519Key() Generator {
	return func() (Secret, error) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return Secret{}, fmt.Errorf("génération de la clé Ed25519 : %w", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			return Secret{}, fmt.Errorf("encodage de la clé Ed25519 : %w", err)
		}
		return NewSecret(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))), nil
	}
}

// SSHKeyPair est la valeur JSON portée par le Secret produit par
// GenerateSSHKeyPair : clé privée OpenSSH et ligne authorized_keys publique.
type SSHKeyPair struct {
	PrivateKeyOpenSSH   string `json:"private_key_openssh"`
	PublicKeyAuthorized string `json:"public_key_authorized"`
}

// GenerateSSHKeyPair produit une paire de clés SSH Ed25519 (docs/06-secrets-etat.md).
func GenerateSSHKeyPair() Generator {
	return func() (Secret, error) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return Secret{}, fmt.Errorf("génération de la paire SSH Ed25519 : %w", err)
		}
		block, err := ssh.MarshalPrivateKey(priv, "")
		if err != nil {
			return Secret{}, fmt.Errorf("encodage de la clé privée SSH : %w", err)
		}
		sshPub, err := ssh.NewPublicKey(pub)
		if err != nil {
			return Secret{}, fmt.Errorf("encodage de la clé publique SSH : %w", err)
		}
		pair := SSHKeyPair{
			PrivateKeyOpenSSH:   string(pem.EncodeToMemory(block)),
			PublicKeyAuthorized: string(ssh.MarshalAuthorizedKey(sshPub)),
		}
		value, err := json.Marshal(pair)
		if err != nil {
			return Secret{}, fmt.Errorf("encodage de la paire SSH : %w", err)
		}
		return NewSecret(string(value)), nil
	}
}
