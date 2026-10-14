package config

import (
	"errors"
	"os"
)

type Config struct {
	AppEnv      string
	HTTPAddr    string
	BaseURL     string
	DatabaseURL string
	PhotoDir    string
	TrustProxy  bool
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
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
