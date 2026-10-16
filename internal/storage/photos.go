// Package storage keeps patrol photos behind a small interface, implemented by the
// local disk (LocalStore) and by any S3-compatible bucket (S3Store).
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type PhotoStore interface {
	Save(ctx context.Context, key string, r io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// LocalStore writes photos to Dir/<key>. Keys look like "2026/10/07/<uuid>.jpg".
type LocalStore struct {
	Dir string
}

var ErrInvalidKey = errors.New("invalid storage key")

// validKey cleans a key and rejects anything that could escape the store's root,
// returning it slash-separated.
func validKey(key string) (string, error) {
	clean := path.Clean(filepath.ToSlash(key))
	if key == "" || path.IsAbs(clean) || filepath.IsAbs(key) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	return clean, nil
}

// ContentType derives the media type from the storage key; photos are only ever
// stored as .jpg or .webp, chosen from the sniffed content.
func ContentType(key string) string {
	if strings.HasSuffix(key, ".webp") {
		return "image/webp"
	}
	return "image/jpeg"
}

// path resolves a key below Dir.
func (s LocalStore) path(key string) (string, error) {
	clean, err := validKey(key)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.Dir, filepath.FromSlash(clean)), nil
}

// Save writes to a temporary file in the target directory and renames it, so a
// crash never leaves a half-written photo under its final name.
func (s LocalStore) Save(_ context.Context, key string, r io.Reader) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (s LocalStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

// Delete removes a photo. A missing file is reported as an error satisfying
// errors.Is(err, fs.ErrNotExist) so callers can treat it as already deleted.
func (s LocalStore) Delete(_ context.Context, key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	return os.Remove(p)
}
