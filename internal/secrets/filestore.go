// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/WhiteRoseLK/genesis/internal/atomicfile"
)

// FileStore is the `file` backend of iteration 1 (docs/06-secrets-state.md):
// one age-encrypted file per secret, plaintext metadata without the value,
// under state_dir/secrets/.
type FileStore struct {
	Identity *age.X25519Identity
	stateDir string
}

// NewFileStore builds a FileStore rooted at stateDir, encrypting with the
// identity master key.
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
		return nil // already present: idempotent, never regenerated.
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking %s: %w", s.secretPath(ref), err)
	}

	value, err := gen()
	if err != nil {
		return fmt.Errorf("generating secret %s: %w", ref, err)
	}
	return s.Put(ctx, ref, value, meta)
}

func (s *FileStore) Put(_ context.Context, ref Ref, value Secret, meta Meta) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.secretPath(ref)), 0o700); err != nil {
		return fmt.Errorf("creating the directory for %s: %w", ref, err)
	}

	ciphertext, err := s.encrypt(value.ExposeSecret())
	if err != nil {
		return fmt.Errorf("chiffrement de %s: %w", ref, err)
	}
	if err := atomicfile.Write(s.secretPath(ref), ciphertext, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", ref, err)
	}

	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the metadata of %s: %w", ref, err)
	}
	if err := atomicfile.Write(s.metaPath(ref), metaJSON, 0o600); err != nil {
		return fmt.Errorf("writing the metadata of %s: %w", ref, err)
	}
	return nil
}

func (s *FileStore) Get(_ context.Context, ref Ref) (Secret, error) {
	if err := ref.Validate(); err != nil {
		return Secret{}, err
	}
	ciphertext, err := os.ReadFile(s.secretPath(ref))
	if os.IsNotExist(err) {
		return Secret{}, fmt.Errorf("secret %s: not found", ref)
	}
	if err != nil {
		return Secret{}, fmt.Errorf("lecture de %s: %w", ref, err)
	}
	plaintext, err := s.decrypt(ciphertext)
	if err != nil {
		return Secret{}, fmt.Errorf("decrypting %s: %w", ref, err)
	}
	return NewSecret(plaintext), nil
}

func (s *FileStore) GetMeta(_ context.Context, ref Ref) (Meta, error) {
	if err := ref.Validate(); err != nil {
		return Meta{}, err
	}
	raw, err := os.ReadFile(s.metaPath(ref))
	if os.IsNotExist(err) {
		return Meta{}, fmt.Errorf("secret %s: not found", ref)
	}
	if err != nil {
		return Meta{}, fmt.Errorf("reading the metadata of %s: %w", ref, err)
	}
	var meta Meta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return Meta{}, fmt.Errorf("invalid metadata for %s: %w", ref, err)
	}
	return meta, nil
}

func (s *FileStore) List(_ context.Context, prefix string) ([]Entry, error) {
	// os.Root: the walk cannot leave the secrets directory, neither through a
	// symbolic link nor through a prefix containing "..".
	root, err := os.OpenRoot(s.secretsDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // nothing to list
	}
	if err != nil {
		return nil, fmt.Errorf("listing secrets: %w", err)
	}
	defer func() { _ = root.Close() }()
	fsys := root.FS()

	start := "."
	if prefix != "" {
		start = path.Clean(prefix)
	}

	var entries []Entry
	err = fs.WalkDir(fsys, start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == start {
				return nil // nothing to list
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".meta.json") {
			return nil
		}
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("lecture de %s: %w", p, err)
		}
		var meta Meta
		if err := json.Unmarshal(raw, &meta); err != nil {
			return fmt.Errorf("invalid metadata in %s: %w", p, err)
		}
		entries = append(entries, Entry{Ref: Ref(strings.TrimSuffix(p, ".meta.json")), Meta: meta})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing secrets under %q: %w", prefix, err)
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
