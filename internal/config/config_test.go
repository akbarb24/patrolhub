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

func TestBaseURLMustBeAbsolute(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	for _, bad := range []string{"ronda.example.com", "/relative", "ftp://x"} {
		t.Setenv("BASE_URL", bad)
		if _, err := Load(); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
	t.Setenv("BASE_URL", "https://ronda.example.com/app")
	c, err := Load()
	if err != nil || c.Origin() != "https://ronda.example.com" {
		t.Fatalf("got %q %v", c.Origin(), err)
	}
}

func TestLoadWindowClocks(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("BASE_URL", "http://localhost:8080")
	t.Setenv("WINDOW_OPEN", "21:30")
	t.Setenv("WINDOW_CLOSE", "05:00")
	c, err := Load()
	if err != nil || c.WindowOpen != "21:30" || c.WindowClose != "05:00" {
		t.Fatalf("got %+v %v", c, err)
	}
}
