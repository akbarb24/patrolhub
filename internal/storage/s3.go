package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/akbarbinaji/patrolhub/internal/config"
)

// s3Timeout bounds every bucket call. Save runs inside the report transaction,
// which holds the night row lock, so a hanging bucket must not hold it for long.
const s3Timeout = 15 * time.Second

// S3Store keeps photos as private objects in an S3-compatible bucket, under the
// same keys LocalStore uses as relative paths.
type S3Store struct {
	client *minio.Client
	bucket string
}

// NewS3Store connects to the bucket and checks that it exists; it never creates it.
func NewS3Store(ctx context.Context, cfg config.S3) (*S3Store, error) {
	client, err := newMinioClient(cfg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s3Timeout)
	defer cancel()
	ok, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket %q: %w", cfg.Bucket, err)
	}
	if !ok {
		return nil, fmt.Errorf("bucket %q does not exist", cfg.Bucket)
	}
	return &S3Store{client: client, bucket: cfg.Bucket}, nil
}

func newMinioClient(cfg config.S3) (*minio.Client, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse S3 endpoint: %w", err)
	}
	lookup := minio.BucketLookupDNS
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	return minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure:       u.Scheme == "https",
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
}

// Save uploads the whole photo in one request; photos are at most about 1 MB.
func (s *S3Store) Save(ctx context.Context, key string, r io.Reader) error {
	key, err := validKey(key)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s3Timeout)
	defer cancel()
	_, err = s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: ContentType(key)})
	return err
}

// Open reads the whole object up front: GetObject is lazy, and a missing key
// must fail here (as fs.ErrNotExist, like LocalStore) rather than on first Read.
func (s *S3Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	key, err := validKey(key)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s3Timeout)
	defer cancel()
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, s.mapErr(key, err)
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, s.mapErr(key, err)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// Delete removes the object. S3 reports success for a key that is already gone,
// which callers treat the same as fs.ErrNotExist.
func (s *S3Store) Delete(ctx context.Context, key string) error {
	key, err := validKey(key)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s3Timeout)
	defer cancel()
	return s.mapErr(key, s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}))
}

func (s *S3Store) mapErr(key string, err error) error {
	if err == nil {
		return nil
	}
	var resp minio.ErrorResponse
	if errors.As(err, &resp) && resp.Code == "NoSuchKey" {
		return fmt.Errorf("photo %q: %w", key, fs.ErrNotExist)
	}
	return err
}
