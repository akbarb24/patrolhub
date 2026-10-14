package httpx

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	a, admin := closedNight(t, false)
	b := a.anonymous()
	for _, p := range []string{"/recap/2026/10", "/static/css/app.css", "/healthz", "/nope", "/admin/login", "/", "/patrol/tonight"} {
		resp, _ := b.do("GET", p, nil)
		for h, want := range map[string]string{"X-Content-Type-Options": "nosniff", "Referrer-Policy": "same-origin", "X-Frame-Options": "DENY"} {
			if got := resp.Header.Get(h); got != want {
				t.Errorf("%s %s: %q, want %q", p, h, got, want)
			}
		}
	}
	resp, _ := admin.do("GET", "/admin/photos/1", nil)
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Error("admin photo lacks security headers")
	}
}

func TestOversizedFormsAreRejected(t *testing.T) {
	a := newApp(t, wibTime(2026, 10, 7, 12, 0))
	b := a.anonymous()
	_, page := b.do("GET", "/admin/login", nil)
	token := csrfField.FindStringSubmatch(page)[1]

	small := url.Values{"csrf_token": {token}, "username": {"admin"}, "password": {"x"}}
	if resp, _ := b.do("POST", "/admin/login", small); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("normal form: %d", resp.StatusCode)
	}
	big := url.Values{"csrf_token": {token}, "username": {"admin"}, "password": {strings.Repeat("a", 100<<10)}}
	resp, _ := b.do("POST", "/admin/login", big)
	if resp.StatusCode < 400 || resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("100 KB form was processed: %d", resp.StatusCode)
	}
}

func forwarded(b *browser, path string, ip string) {
	b.t.Helper()
	_, page := b.do("GET", "/patrol/tonight", nil)
	m := hxToken.FindStringSubmatch(page)
	if m == nil {
		b.t.Fatal("no token")
	}
	b.doRaw("POST", path, strings.NewReader("note=halo"), map[string]string{
		"X-CSRF-Token": m[1], "Content-Type": "application/x-www-form-urlencoded", "X-Forwarded-For": ip,
	})
}

func auditIP(t *testing.T, a *app) string {
	t.Helper()
	var ip string
	if err := a.pool.QueryRow(context.Background(), `SELECT ip FROM audit_log WHERE action = 'update_situation' ORDER BY id DESC LIMIT 1`).Scan(&ip); err != nil {
		t.Fatal(err)
	}
	return ip
}

func TestForwardedForIsIgnoredUnlessProxyIsTrusted(t *testing.T) {
	setup := func(trust bool) (*app, *browser) {
		a := newApp(t, wibTime(2026, 10, 7, 22, 30), func(d *Deps) { d.Config.TrustProxy = trust })
		return a, a.anonymous()
	}
	a, b := setup(false)
	forwarded(b, "/patrol/tonight/situation", "198.51.100.77")
	if got := auditIP(t, a); got != "127.0.0.1" {
		t.Fatalf("TRUST_PROXY=false recorded %q", got)
	}

	a, b = setup(true)
	forwarded(b, "/patrol/tonight/situation", "198.51.100.77")
	if got := auditIP(t, a); got != "198.51.100.77" {
		t.Fatalf("TRUST_PROXY=true recorded %q", got)
	}
}

type captureHandler struct{ buf *bytes.Buffer }

func (c captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (c captureHandler) Handle(_ context.Context, r slog.Record) error {
	c.buf.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		c.buf.WriteString(" " + a.Key + "=" + a.Value.String())
		return true
	})
	c.buf.WriteString("\n")
	return nil
}
func (c captureHandler) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c captureHandler) WithGroup(string) slog.Handler      { return c }

func TestRequestLogsNeverContainSecrets(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(captureHandler{&buf}))
	t.Cleanup(func() { slog.SetDefault(old) })

	a := newApp(t, wibTime(2026, 10, 7, 12, 0))
	admin := a.loggedIn()
	admin.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {"A-1"}, "phone": {"081299990000"}}, http.StatusSeeOther)
	b := a.anonymous()
	b.post("/admin/login", "/admin/login", url.Values{"username": {"admin"}, "password": {"Sup3rSecret-Pass"}})
	b.do("GET", "/recap/2026/10?token=abc123", nil)

	logs := buf.String()
	if !strings.Contains(logs, "request method=POST path=/admin/login status=401") {
		t.Fatalf("expected a request line for the failed login, got:\n%s", logs)
	}
	for _, secret := range []string{"Sup3rSecret-Pass", "correct-horse-battery", "081299990000", "abc123", "token=", "session=", "csrf_token"} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs leak %q", secret)
		}
	}
}
