// Package storage keeps patrol photos behind a small interface so the local disk
// store can later be swapped for S3/MinIO.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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

// path resolves a key below Dir and rejects anything that could escape it.
func (s LocalStore) path(key string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(key))
	if key == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	return filepath.Join(s.Dir, clean), nil
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
