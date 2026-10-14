package config

import "testing"

func TestLoadRequiresDatabaseAndBaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("BASE_URL", "https://x")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for empty DATABASE_URL")
	}
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("BASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for empty BASE_URL")
	}
	t.Setenv("BASE_URL", "https://x")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" || c.TrustProxy {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}
