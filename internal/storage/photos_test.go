package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := LocalStore{Dir: dir}
	ctx := context.Background()
	key := "2026/10/07/abc.jpg"

	if err := s.Save(ctx, key, strings.NewReader("pixels")); err != nil {
		t.Fatal(err)
	}
	r, err := s.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r)
	r.Close()
	if string(data) != "pixels" {
		t.Fatalf("got %q", data)
	}

	// No temporary files are left next to the photo.
	entries, _ := os.ReadDir(filepath.Join(dir, "2026/10/07"))
	if len(entries) != 1 || entries[0].Name() != "abc.jpg" {
		t.Fatalf("directory contents: %v", entries)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, key); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := s.Open(ctx, key); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("open after delete: %v", err)
	}
}

func TestLocalStoreRejectsEscapingKeys(t *testing.T) {
	s := LocalStore{Dir: t.TempDir()}
	for _, key := range []string{"", ".", "..", "../x.jpg", "a/../../x.jpg", "/etc/passwd"} {
		if err := s.Save(context.Background(), key, strings.NewReader("x")); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Save(%q): %v", key, err)
		}
		if _, err := s.Open(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Open(%q): %v", key, err)
		}
		if err := s.Delete(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Delete(%q): %v", key, err)
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestFailedSaveLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	s := LocalStore{Dir: dir}
	if err := s.Save(context.Background(), "a/b.jpg", failingReader{}); err == nil {
		t.Fatal("expected error")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "a"))
	if len(entries) != 0 {
		t.Fatalf("leftovers: %v", entries)
	}
}
