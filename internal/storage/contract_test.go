package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"

	"github.com/akbarbinaji/patrolhub/internal/config"
)

// testPhotoStore checks the behaviour callers rely on, whatever the backend.
// Keys are created under prefix so runs against a shared bucket do not collide.
func testPhotoStore(t *testing.T, s PhotoStore, prefix string) {
	t.Helper()
	ctx := context.Background()
	key := prefix + "2026/10/07/abc.jpg"

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

	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, key); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("open after delete: %v", err)
	}
	// The retention job treats both as "already gone".
	if err := s.Delete(ctx, key); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("second delete: %v", err)
	}

	for _, bad := range []string{"", ".", "..", "../x.jpg", "a/../../x.jpg", "/etc/passwd"} {
		if err := s.Save(ctx, bad, strings.NewReader("x")); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Save(%q): %v", bad, err)
		}
		if _, err := s.Open(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Open(%q): %v", bad, err)
		}
		if err := s.Delete(ctx, bad); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Delete(%q): %v", bad, err)
		}
	}
}

func TestLocalStoreContract(t *testing.T) {
	testPhotoStore(t, LocalStore{Dir: t.TempDir()}, "")
}

// TestS3StoreContract runs only when S3_TEST_ENDPOINT points at a test server,
// e.g. the SeaweedFS from docker-compose.yml (whose dev credentials are the defaults):
//
//	S3_TEST_ENDPOINT=http://localhost:8333 go test ./internal/storage
//
// It creates the bucket S3_TEST_BUCKET (default "patrolhub-test") if needed.
func TestS3StoreContract(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_TEST_ENDPOINT not set")
	}
	cfg := config.S3{
		Bucket:          envOr("S3_TEST_BUCKET", "patrolhub-test"),
		Endpoint:        endpoint,
		AccessKeyID:     envOr("S3_TEST_ACCESS_KEY_ID", "dev"),
		SecretAccessKey: envOr("S3_TEST_SECRET_ACCESS_KEY", "dev-secret"),
		Region:          envOr("S3_TEST_REGION", "us-east-1"),
		PathStyle:       os.Getenv("S3_TEST_PATH_STYLE") != "false",
	}
	ctx := context.Background()

	// The constructor refuses a missing bucket.
	missing := cfg
	missing.Bucket = "patrolhub-missing-" + uuid.NewString()[:8]
	if _, err := NewS3Store(ctx, missing); err == nil {
		t.Fatal("expected error for a missing bucket")
	}

	s := mustS3Store(t, ctx, cfg)
	prefix := "test-" + uuid.NewString() + "/"
	testPhotoStore(t, s, prefix)

	if err := s.Save(ctx, prefix+"a.webp", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	defer s.Delete(ctx, prefix+"a.webp")
	info, err := s.client.StatObject(ctx, cfg.Bucket, prefix+"a.webp", minio.StatObjectOptions{})
	if err != nil || info.ContentType != "image/webp" {
		t.Fatalf("content type %q, err %v", info.ContentType, err)
	}
}

// mustS3Store creates the test bucket when it is missing, then opens the store.
func mustS3Store(t *testing.T, ctx context.Context, cfg config.S3) *S3Store {
	t.Helper()
	client, err := newMinioClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			t.Fatalf("create test bucket: %v", err)
		}
	}
	s, err := NewS3Store(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
