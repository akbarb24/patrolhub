package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	AppEnv      string
	HTTPAddr    string
	BaseURL     string
	DatabaseURL string
	PhotoDir    string
	TrustProxy  bool

	// S3 holds the object storage settings; when S3.Enabled() photos go there
	// instead of PhotoDir.
	S3 S3

	// WindowOpen and WindowClose are optional defaults for the report window,
	// applied once before go-live (see service.SettingsService.SeedWindow).
	// Raw HH:MM strings; parsed in main because domain already imports config.
	WindowOpen  string
	WindowClose string
}

func (c Config) IsProduction() bool { return c.AppEnv == "production" }

// Load reads configuration from environment variables.
func Load() (Config, error) {
	c := Config{
		AppEnv:      getenv("APP_ENV", "development"),
		HTTPAddr:    getenv("HTTP_ADDR", ":8080"),
		BaseURL:     os.Getenv("BASE_URL"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		PhotoDir:    getenv("PHOTO_DIR", "./data/photos"),
		TrustProxy:  os.Getenv("TRUST_PROXY") == "true",
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	if c.BaseURL == "" {
		return c, errors.New("BASE_URL is required")
	}
	if u, err := url.Parse(c.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return c, fmt.Errorf("BASE_URL must be an absolute http(s) URL, got %q", c.BaseURL)
	}
	if err := c.S3.load(); err != nil {
		return c, err
	}
	c.WindowOpen = os.Getenv("WINDOW_OPEN")
	c.WindowClose = os.Getenv("WINDOW_CLOSE")
	return c, nil
}

// S3 configures an S3-compatible photo bucket (Tigris on Fly, SeaweedFS, MinIO).
// The variable names are the ones `fly storage create` sets as secrets.
type S3 struct {
	Bucket          string // BUCKET_NAME; empty means photos stay on the local disk
	Endpoint        string // AWS_ENDPOINT_URL_S3, absolute http(s) URL
	AccessKeyID     string // AWS_ACCESS_KEY_ID
	SecretAccessKey string // AWS_SECRET_ACCESS_KEY
	Region          string // AWS_REGION, default "auto"
	PathStyle       bool   // S3_PATH_STYLE=true for SeaweedFS/MinIO
}

func (s S3) Enabled() bool { return s.Bucket != "" }

func (s *S3) load() error {
	*s = S3{
		Bucket:          os.Getenv("BUCKET_NAME"),
		Endpoint:        os.Getenv("AWS_ENDPOINT_URL_S3"),
		AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		Region:          getenv("AWS_REGION", "auto"),
		PathStyle:       os.Getenv("S3_PATH_STYLE") == "true",
	}
	if !s.Enabled() {
		return nil
	}
	if s.Endpoint == "" || s.AccessKeyID == "" || s.SecretAccessKey == "" {
		return errors.New("BUCKET_NAME is set, so AWS_ENDPOINT_URL_S3, AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are required")
	}
	if u, err := url.Parse(s.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("AWS_ENDPOINT_URL_S3 must be an absolute http(s) URL, got %q", s.Endpoint)
	}
	return nil
}

// TonightURL is the address printed on the QR code.
func (c Config) TonightURL() string {
	return strings.TrimRight(c.BaseURL, "/") + "/patrol/tonight"
}

// Origin returns scheme://host of BASE_URL, the origin browsers send in the Origin header.
func (c Config) Origin() string {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
