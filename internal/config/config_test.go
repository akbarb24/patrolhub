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

func TestLoadS3(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("BASE_URL", "http://localhost:8080")
	for _, k := range []string{"BUCKET_NAME", "AWS_ENDPOINT_URL_S3", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_REGION", "S3_PATH_STYLE"} {
		t.Setenv(k, "")
	}
	c, err := Load()
	if err != nil || c.S3.Enabled() {
		t.Fatalf("S3 should be off without BUCKET_NAME: %+v %v", c.S3, err)
	}

	t.Setenv("BUCKET_NAME", "photos")
	if _, err := Load(); err == nil {
		t.Fatal("expected error: endpoint and keys missing")
	}

	t.Setenv("AWS_ACCESS_KEY_ID", "key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	for _, bad := range []string{"", "localhost:8333", "ftp://x"} {
		t.Setenv("AWS_ENDPOINT_URL_S3", bad)
		if _, err := Load(); err == nil {
			t.Errorf("endpoint %q: expected error", bad)
		}
	}

	t.Setenv("AWS_ENDPOINT_URL_S3", "http://localhost:8333")
	t.Setenv("S3_PATH_STYLE", "true")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.S3.Enabled() || c.S3.Region != "auto" || !c.S3.PathStyle || c.S3.Bucket != "photos" {
		t.Fatalf("got %+v", c.S3)
	}
}

func TestLoadWhatsApp(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("BASE_URL", "http://localhost:8080")
	for _, k := range []string{"WA_API_URL", "WA_API_USER", "WA_API_PASS", "WA_GROUP_ID"} {
		t.Setenv(k, "")
	}
	c, err := Load()
	if err != nil || c.WhatsApp.Enabled() {
		t.Fatalf("WhatsApp should be off without WA_API_URL: %+v %v", c.WhatsApp, err)
	}

	t.Setenv("WA_GROUP_ID", "120363000000000000@g.us")
	for _, bad := range []string{"patrolhub-wa.flycast", "ftp://x"} {
		t.Setenv("WA_API_URL", bad)
		if _, err := Load(); err == nil {
			t.Errorf("url %q: expected error", bad)
		}
	}

	t.Setenv("WA_API_URL", "http://patrolhub-wa.flycast")
	for _, bad := range []string{"", "6281234567890@s.whatsapp.net"} {
		t.Setenv("WA_GROUP_ID", bad)
		if _, err := Load(); err == nil {
			t.Errorf("group %q: expected error", bad)
		}
	}

	t.Setenv("WA_GROUP_ID", "120363000000000000@g.us")
	t.Setenv("WA_API_USER", "patrolhub")
	t.Setenv("WA_API_PASS", "secret")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.WhatsApp.Enabled() || c.WhatsApp.User != "patrolhub" || c.WhatsApp.Password != "secret" {
		t.Fatalf("got %+v", c.WhatsApp)
	}
}
