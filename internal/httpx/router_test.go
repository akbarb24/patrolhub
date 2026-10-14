package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/alexedwards/scs/v2"

	"github.com/akbarbinaji/patrolhub/internal/config"
)

func newTestRouter(s SettingsStore) http.Handler {
	h, err := NewRouter(Deps{
		Config:   config.Config{AppEnv: "development", BaseURL: "https://ronda.example.com"},
		Sessions: scs.New(),
		Settings: s,
		Admins:   fakeAdmins{},
		Static:   fstest.MapFS{"js/htmx.min.js": {Data: []byte("//js")}},
	})
	if err != nil {
		panic(err)
	}
	return h
}

func do(h http.Handler, method, path string, hdr map[string]string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthz(t *testing.T) {
	rec := do(newTestRouter(&fakeSettings{}), "GET", "/healthz", nil, nil)
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
}

func TestRootRedirectsToRecap(t *testing.T) {
	rec := do(newTestRouter(&fakeSettings{}), "GET", "/", nil, nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/recap" {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestPublicLayout(t *testing.T) {
	rec := do(newTestRouter(&fakeSettings{}), "GET", "/admin/login", nil, nil)
	body := rec.Body.String()
	for _, want := range []string{`lang="id"`, "PatrolHub · RT 02", "Masuk Admin", "/static/css/app.css", "X-CSRF-Token", `name="viewport"`} {
		if rec.Code != 200 || !strings.Contains(body, want) {
			t.Fatalf("status %d, missing %q in:\n%s", rec.Code, want, body)
		}
	}
}

func TestPageSurvivesSettingsFailure(t *testing.T) {
	rec := do(newTestRouter(&fakeSettings{loadErr: context.DeadlineExceeded}), "GET", "/admin/login", nil, nil)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "RT 02") || !strings.Contains(rec.Body.String(), "PatrolHub") {
		t.Fatalf("%d", rec.Code)
	}
}

func TestPostWithoutCSRFTokenIsForbidden(t *testing.T) {
	rec := do(newTestRouter(&fakeSettings{}), "POST", "/recap", nil, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d", rec.Code)
	}
}

func csrfToken(t *testing.T, h http.Handler) (string, []*http.Cookie) {
	t.Helper()
	get := do(h, "GET", "/admin/login", nil, nil)
	m := regexp.MustCompile(`X-CSRF-Token&#34;:&#34;([^&]+)&#34;`).FindStringSubmatch(get.Body.String())
	if m == nil {
		t.Fatalf("no token in page:\n%s", get.Body.String())
	}
	return m[1], get.Result().Cookies()
}

func TestPostWithCSRFTokenPassesCSRFCheck(t *testing.T) {
	h := newTestRouter(&fakeSettings{})
	tok, cookies := csrfToken(t, h)
	// 405 means CSRF passed and only the POST route is missing.
	tests := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"same-origin fetch", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusMethodNotAllowed},
		{"Origin of BASE_URL (behind TLS proxy)", map[string]string{"Origin": "https://ronda.example.com"}, http.StatusMethodNotAllowed},
		{"foreign Origin", map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"no origin proof at all", nil, http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hdr := map[string]string{"X-CSRF-Token": tok}
			for k, v := range tc.hdr {
				hdr[k] = v
			}
			if got := do(h, "POST", "/recap", hdr, cookies).Code; got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestPostWithWrongTokenIsForbidden(t *testing.T) {
	h := newTestRouter(&fakeSettings{})
	_, cookies := csrfToken(t, h)
	hdr := map[string]string{"X-CSRF-Token": "bogus", "Sec-Fetch-Site": "same-origin"}
	if got := do(h, "POST", "/recap", hdr, cookies).Code; got != http.StatusForbidden {
		t.Fatalf("got %d", got)
	}
}

func TestStaticCaching(t *testing.T) {
	rec := do(newTestRouter(&fakeSettings{}), "GET", "/static/js/htmx.min.js", nil, nil)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if !fingerprinted.MatchString("/static/css/app.3f9a1c2b.css") || fingerprinted.MatchString("/static/css/app.css") {
		t.Fatal("fingerprint regexp")
	}
}
