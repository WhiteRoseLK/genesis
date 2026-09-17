// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"

	"genesis/internal/atomicfile"
)

// FileStore est le backend `file` de l'itération 1 (docs/06-secrets-etat.md) :
// un fichier chiffré age par secret, des métadonnées en clair mais sans
// valeur, sous state_dir/secrets/.
type FileStore struct {
	Identity *age.X25519Identity
	stateDir string
}

// NewFileStore construit un FileStore ancré sur stateDir, chiffrant avec la
// clé maîtresse identity.
func NewFileStore(stateDir string, identity *age.X25519Identity) *FileStore {
	return &FileStore{Identity: identity, stateDir: stateDir}
}

var _ Store = (*FileStore)(nil)

func (s *FileStore) Backend() string { return "file" }

func (s *FileStore) secretsDir() string {
	return filepath.Join(s.stateDir, "secrets")
}

func (s *FileStore) secretPath(ref Ref) string {
	segments := append([]string{s.secretsDir()}, ref.Segments()...)
	return filepath.Join(segments...) + ".age"
}

func (s *FileStore) metaPath(ref Ref) string {
	segments := append([]string{s.secretsDir()}, ref.Segments()...)
	return filepath.Join(segments...) + ".meta.json"
}

func (s *FileStore) Ensure(ctx context.Context, ref Ref, gen Generator, meta Meta) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	if _, err := os.Stat(s.secretPath(ref)); err == nil {
		return nil // déjà présent : idempotent, on ne régénère jamais.
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("vérification de %s : %w", s.secretPath(ref), err)
	}

	value, err := gen()
	if err != nil {
		return fmt.Errorf("génération du secret %s : %w", ref, err)
	}
	return s.Put(ctx, ref, value, meta)
}

func (s *FileStore) Put(_ context.Context, ref Ref, value Secret, meta Meta) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.secretPath(ref)), 0o700); err != nil {
		return fmt.Errorf("création du répertoire pour %s : %w", ref, err)
	}

	ciphertext, err := s.encrypt(value.ExposeSecret())
	if err != nil {
		return fmt.Errorf("chiffrement de %s : %w", ref, err)
	}
	if err := atomicfile.Write(s.secretPath(ref), ciphertext, 0o600); err != nil {
		return fmt.Errorf("écriture de %s : %w", ref, err)
	}

	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encodage des métadonnées de %s : %w", ref, err)
	}
	if err := atomicfile.Write(s.metaPath(ref), metaJSON, 0o600); err != nil {
		return fmt.Errorf("écriture des métadonnées de %s : %w", ref, err)
	}
	return nil
}

func (s *FileStore) Get(_ context.Context, ref Ref) (Secret, error) {
	if err := ref.Validate(); err != nil {
		return Secret{}, err
	}
	ciphertext, err := os.ReadFile(s.secretPath(ref))
	if os.IsNotExist(err) {
		return Secret{}, fmt.Errorf("secret %s : introuvable", ref)
	}
	if err != nil {
		return Secret{}, fmt.Errorf("lecture de %s : %w", ref, err)
	}
	plaintext, err := s.decrypt(ciphertext)
	if err != nil {
		return Secret{}, fmt.Errorf("déchiffrement de %s : %w", ref, err)
	}
	return NewSecret(plaintext), nil
}

func (s *FileStore) List(_ context.Context, prefix string) ([]Entry, error) {
	root := s.secretsDir()
	searchRoot := root
	if prefix != "" {
		searchRoot = filepath.Join(root, filepath.FromSlash(prefix))
	}

	var entries []Entry
	err := filepath.WalkDir(searchRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == searchRoot {
				return nil // rien à lister
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".meta.json") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		refStr := strings.TrimSuffix(filepath.ToSlash(rel), ".meta.json")

		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("lecture de %s : %w", path, err)
		}
		var meta Meta
		if err := json.Unmarshal(raw, &meta); err != nil {
			return fmt.Errorf("métadonnées invalides dans %s : %w", path, err)
		}
		entries = append(entries, Entry{Ref: Ref(refStr), Meta: meta})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("liste des secrets sous %q : %w", prefix, err)
	}
	return entries, nil
}

func (s *FileStore) encrypt(plaintext string) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, s.Identity.Recipient())
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(w, plaintext); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *FileStore) decrypt(ciphertext []byte) (string, error) {
	r, err := age.Decrypt(bytes.NewReader(ciphertext), s.Identity)
	if err != nil {
		return "", err
	}
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
