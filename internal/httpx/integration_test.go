package httpx

import (
	"context"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/config"
	"github.com/akbarbinaji/patrolhub/internal/service"
	"github.com/akbarbinaji/patrolhub/internal/storage"
	"github.com/akbarbinaji/patrolhub/internal/testdb"
	"github.com/akbarbinaji/patrolhub/web"
)

// app is the whole application over a throwaway database with a controllable clock.
type app struct {
	t      *testing.T
	pool   *pgxpool.Pool
	now    time.Time
	h      http.Handler
	photos storage.LocalStore

	adminMade bool
}

func (a *app) clock() time.Time { return a.now }

func newApp(t *testing.T, now time.Time, opts ...func(*Deps)) *app {
	t.Helper()
	pool := testdb.New(t)
	a := &app{t: t, pool: pool, now: now, photos: storage.LocalStore{Dir: t.TempDir()}}
	settings := service.NewSettingsService(pool)
	deps := Deps{
		Config:   config.Config{AppEnv: "development", BaseURL: "https://ronda.example.com"},
		Sessions: scs.New(),
		Settings: settings,
		Admins:   service.NewAdminService(pool),
		Members:  service.NewMemberService(pool),
		Schedule: service.NewScheduleService(pool),
		Patrol:   service.NewPatrolService(pool, settings, a.photos),
		Nights:   service.NewNightService(pool, settings, a.photos),
		Fines:    service.NewFineService(pool),
		Recap:    service.NewRecapService(pool, settings),
		Static:   staticFS(t),
		Now:      a.clock,
	}
	for _, o := range opts {
		o(&deps)
	}
	h, err := NewRouter(deps)
	if err != nil {
		t.Fatal(err)
	}
	a.h = h
	return a
}

// loggedIn returns a browser with an admin session.
func (a *app) loggedIn() *browser {
	a.t.Helper()
	if !a.adminMade {
		if _, err := service.NewAdminService(a.pool).Create(context.Background(), "admin", "correct-horse-battery"); err != nil {
			a.t.Fatal(err)
		}
		a.adminMade = true
	}
	b := newBrowser(a.t, a.h)
	resp, _ := b.post("/admin/login", "/admin/login", url.Values{"username": {"admin"}, "password": {"correct-horse-battery"}})
	if resp.StatusCode != http.StatusSeeOther {
		a.t.Fatalf("login status %d", resp.StatusCode)
	}
	return b
}

func (a *app) anonymous() *browser { return newBrowser(a.t, a.h) }

func (b *browser) mustPost(page, action string, form url.Values, wantStatus int) string {
	b.t.Helper()
	resp, body := b.post(page, action, form)
	if resp.StatusCode != wantStatus {
		b.t.Fatalf("POST %s: status %d, want %d\n%s", action, resp.StatusCode, wantStatus, body)
	}
	return body
}

func (b *browser) mustGet(path string) string {
	b.t.Helper()
	resp, body := b.do("GET", path, nil)
	if resp.StatusCode != 200 {
		b.t.Fatalf("GET %s: status %d\n%s", path, resp.StatusCode, body)
	}
	return body
}

func contains(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("response lacks %q", w)
		}
	}
}

func notContains(t *testing.T, body string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(body, n) {
			t.Errorf("response unexpectedly contains %q", n)
		}
	}
}

var wibZone = config.Jakarta

func wibTime(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, wibZone)
}

// staticFS is the real embedded asset tree, so tests can measure page weight.
func staticFS(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(web.Static, "static")
	if err != nil {
		t.Fatal(err)
	}
	return sub
}
