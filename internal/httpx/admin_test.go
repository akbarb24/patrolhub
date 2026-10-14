package httpx

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/service"
)

type fakeSettings struct {
	loadErr error
	saved   *service.Settings
}

func (f *fakeSettings) Load(context.Context) (service.Settings, error) {
	if f.saved != nil {
		return *f.saved, f.loadErr
	}
	return service.Settings{
		RTName: "RT 02", WindowOpen: domain.Clock{Hour: 22}, WindowClose: domain.Clock{Hour: 6},
		FinePerAbsence: 5000, MinPhotos: 3, MaxPhotos: 10,
	}, f.loadErr
}

func (f *fakeSettings) Save(_ context.Context, s service.Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	f.saved = &s
	return nil
}

type fakeAdmins struct{}

func (fakeAdmins) Authenticate(_ context.Context, user, pass string) (int64, error) {
	if user == "budi" && pass == "correct-password" {
		return 7, nil
	}
	return 0, service.ErrInvalidCredentials
}

// browser is a cookie-keeping client that never follows redirects and always sends
// the same-origin proof a real browser would.
type browser struct {
	t   *testing.T
	srv *httptest.Server
	c   *http.Client
}

func newBrowser(t *testing.T, h http.Handler) *browser {
	jar, _ := cookiejar.New(nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &browser{t, srv, &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (b *browser) do(method, path string, form url.Values) (*http.Response, string) {
	b.t.Helper()
	var body io.Reader
	hdr := map[string]string{}
	if form != nil {
		body = strings.NewReader(form.Encode())
		hdr["Content-Type"] = "application/x-www-form-urlencoded"
	}
	return b.doRaw(method, path, body, hdr)
}

// doRaw sends any body with extra headers (and the same-origin proof a browser adds).
func (b *browser) doRaw(method, path string, body io.Reader, hdr map[string]string) (*http.Response, string) {
	b.t.Helper()
	req, _ := http.NewRequest(method, b.srv.URL+path, body)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, string(data)
}

var csrfField = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// post fetches formPage to obtain a CSRF token, then submits form to action.
func (b *browser) post(formPage, action string, form url.Values) (*http.Response, string) {
	b.t.Helper()
	_, page := b.do("GET", formPage, nil)
	m := csrfField.FindStringSubmatch(page)
	if m == nil {
		b.t.Fatalf("no csrf field on %s:\n%s", formPage, page)
	}
	form.Set("csrf_token", m[1])
	return b.do("POST", action, form)
}

func (b *browser) login() {
	b.t.Helper()
	resp, _ := b.post("/admin/login", "/admin/login", url.Values{"username": {"budi"}, "password": {"correct-password"}})
	if resp.StatusCode != http.StatusSeeOther {
		b.t.Fatalf("login status %d", resp.StatusCode)
	}
}

func TestAdminPagesRedirectToLoginWhenAnonymous(t *testing.T) {
	b := newBrowser(t, newTestRouter(&fakeSettings{}))
	for _, path := range []string{"/admin", "/admin/", "/admin/nights", "/admin/settings", "/admin/qr", "/admin/qr.png"} {
		resp, _ := b.do("GET", path, nil)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/login" {
			t.Errorf("%s: %d -> %q", path, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
}

func TestLoginWrongCredentials(t *testing.T) {
	b := newBrowser(t, newTestRouter(&fakeSettings{}))
	for _, creds := range []url.Values{
		{"username": {"budi"}, "password": {"wrong"}},
		{"username": {"nobody"}, "password": {"correct-password"}},
	} {
		resp, body := b.post("/admin/login", "/admin/login", creds)
		if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "Username atau password salah.") {
			t.Errorf("%v: %d", creds, resp.StatusCode)
		}
	}
	if resp, _ := b.do("GET", "/admin/settings", nil); resp.StatusCode != http.StatusSeeOther {
		t.Error("failed login must not create a session")
	}
}

func TestLoginLogoutFlow(t *testing.T) {
	b := newBrowser(t, newTestRouter(&fakeSettings{}))
	b.login()

	if resp, body := b.do("GET", "/admin/settings", nil); resp.StatusCode != 200 ||
		resp.Header.Get("Cache-Control") != "private, no-store" || !strings.Contains(body, "Keluar") {
		t.Fatalf("settings after login: %d %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	if resp, _ := b.do("GET", "/admin/login", nil); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/nights" {
		t.Errorf("login page while logged in: %d", resp.StatusCode)
	}

	resp, _ := b.post("/admin/settings", "/admin/logout", url.Values{})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/login" {
		t.Fatalf("logout: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, _ := b.do("GET", "/admin/settings", nil); resp.StatusCode != http.StatusSeeOther {
		t.Error("session must be gone after logout")
	}
}

func TestLoginRateLimit(t *testing.T) {
	b := newBrowser(t, newTestRouter(&fakeSettings{}))
	var last int
	for i := 0; i < 11; i++ {
		resp, _ := b.do("GET", "/admin/login", nil)
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("11th request got %d", last)
	}
}

func TestRateLimitIgnoresForgedForwardedFor(t *testing.T) {
	b := newBrowser(t, newTestRouter(&fakeSettings{})) // TRUST_PROXY is off
	var last int
	for i := 0; i < 11; i++ {
		req, _ := http.NewRequest("GET", b.srv.URL+"/admin/login", nil)
		req.Header.Set("X-Forwarded-For", "10.0.0."+string(rune('1'+i)))
		resp, err := b.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("forged X-Forwarded-For evaded the limit: %d", last)
	}
}

func TestSettingsPageAndSave(t *testing.T) {
	fs := &fakeSettings{}
	b := newBrowser(t, newTestRouter(fs))
	b.login()

	_, body := b.do("GET", "/admin/settings", nil)
	for _, want := range []string{`value="RT 02"`, `value="22:00"`, `value="06:00"`, `value="5000"`} {
		if !strings.Contains(body, want) {
			t.Errorf("form lacks %s", want)
		}
	}

	good := url.Values{
		"rt_name": {"RT 05"}, "window_open": {"21:30"}, "window_close": {"05:00"},
		"fine_per_absence": {"7500"}, "min_photos": {"2"}, "max_photos": {"8"}, "go_live_date": {"2026-10-12"},
	}
	resp, _ := b.post("/admin/settings", "/admin/settings", good)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save status %d", resp.StatusCode)
	}
	if fs.saved == nil || fs.saved.RTName != "RT 05" || fs.saved.FinePerAbsence != 7500 ||
		fs.saved.WindowOpen != (domain.Clock{Hour: 21, Min: 30}) || fs.saved.GoLiveDate == nil || fs.saved.GoLiveDate.String() != "2026-10-12" {
		t.Fatalf("saved %+v", fs.saved)
	}
	_, body = b.do("GET", "/admin/settings", nil)
	if !strings.Contains(body, "Pengaturan tersimpan.") {
		t.Error("expected flash message")
	}
	_, body = b.do("GET", "/admin/settings", nil)
	if strings.Contains(body, "Pengaturan tersimpan.") {
		t.Error("flash must show only once")
	}
}

func TestSettingsValidationShownUnderField(t *testing.T) {
	tests := []struct {
		name    string
		change  map[string]string
		field   string
		message string
	}{
		{"bad clock", map[string]string{"window_open": "tengah malam"}, "window_open", "Jam buka harus berformat HH:MM."},
		{"not a number", map[string]string{"fine_per_absence": "lima ribu"}, "fine_per_absence", "Masukkan angka bulat."},
		{"bad date", map[string]string{"go_live_date": "12/10/2026"}, "go_live_date", "Tanggal harus berformat"},
		{"open before close", map[string]string{"window_open": "05:00"}, "window_open", "Jam buka harus lebih besar"},
		{"negative fine", map[string]string{"fine_per_absence": "-1"}, "fine_per_absence", "Denda tidak boleh negatif."},
		{"max photos too high", map[string]string{"max_photos": "21"}, "max_photos", "tidak boleh lebih dari 20"},
		{"empty name", map[string]string{"rt_name": " "}, "rt_name", "Nama RT wajib diisi."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := &fakeSettings{}
			b := newBrowser(t, newTestRouter(fs))
			b.login()
			form := url.Values{
				"rt_name": {"RT 02"}, "window_open": {"22:00"}, "window_close": {"06:00"},
				"fine_per_absence": {"5000"}, "min_photos": {"3"}, "max_photos": {"10"}, "go_live_date": {""},
			}
			for k, v := range tc.change {
				form.Set(k, v)
			}
			resp, body := b.post("/admin/settings", "/admin/settings", form)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if fs.saved != nil {
				t.Fatal("invalid settings were saved")
			}
			if !strings.Contains(body, tc.message) || !strings.Contains(body, `id="`+tc.field+`-error"`) {
				t.Errorf("message %q not shown under %s", tc.message, tc.field)
			}
			for k, v := range tc.change { // submitted input is echoed back unchanged
				if strings.TrimSpace(v) != "" && !strings.Contains(body, `value="`+strings.TrimSpace(v)+`"`) && k != "rt_name" {
					t.Errorf("value %q for %s not echoed", v, k)
				}
			}
		})
	}
}

func TestQRPageAndPNG(t *testing.T) {
	b := newBrowser(t, newTestRouter(&fakeSettings{}))
	b.login()

	_, body := b.do("GET", "/admin/qr", nil)
	for _, want := range []string{"Laporan Ronda RT 02", "/admin/qr.png", "Pindai pukul 22.00–06.00 untuk mengisi laporan ronda", "https://ronda.example.com/patrol/tonight", "size: A5"} {
		if !strings.Contains(body, want) {
			t.Errorf("QR page lacks %q", want)
		}
	}
	resp, png := b.do("GET", "/admin/qr.png", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !strings.HasPrefix(png, "\x89PNG") {
		t.Fatalf("qr.png: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}
