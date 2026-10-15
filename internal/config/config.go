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
	c.WindowOpen = os.Getenv("WINDOW_OPEN")
	c.WindowClose = os.Getenv("WINDOW_CLOSE")
	return c, nil
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
