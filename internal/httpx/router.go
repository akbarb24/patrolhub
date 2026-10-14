// Package httpx contains the HTTP layer: router, middleware and handlers.
// Handlers talk to services, never to the database directly.
package httpx

import (
	"context"
	"io/fs"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/akbarbinaji/patrolhub/internal/config"
	"github.com/akbarbinaji/patrolhub/internal/service"
	"github.com/akbarbinaji/patrolhub/internal/view"
)

// SettingsLoader is what the page middleware needs from the settings service.
type SettingsLoader interface {
	Load(ctx context.Context) (service.Settings, error)
}

// SettingsStore is the settings service as the admin handlers use it.
type SettingsStore interface {
	SettingsLoader
	Save(ctx context.Context, s service.Settings) error
}

// Authenticator checks admin credentials; it returns service.ErrInvalidCredentials on mismatch.
type Authenticator interface {
	Authenticate(ctx context.Context, username, password string) (int64, error)
}

type Deps struct {
	Config   config.Config
	Sessions *scs.SessionManager
	Settings SettingsStore
	Admins   Authenticator
	Members  *service.MemberService
	Schedule *service.ScheduleService
	Static   fs.FS            // root of the embedded static assets
	Now      func() time.Time // injectable clock; defaults to time.Now
}

func NewRouter(d Deps) (http.Handler, error) {
	if d.Now == nil {
		d.Now = time.Now
	}
	csrfMiddleware, err := csrf(d.Config.IsProduction(), d.Config.Origin())
	if err != nil {
		return nil, err
	}

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	if d.Config.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(
		middleware.Logger,
		middleware.Recoverer,
		middleware.Compress(5),
		d.Sessions.LoadAndSave,
		csrfMiddleware,
	)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	r.Handle("/static/*", cacheStatic(http.StripPrefix("/static/", http.FileServerFS(d.Static))))

	r.Group(func(r chi.Router) {
		r.Use(pageData(d.Settings))

		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/recap", http.StatusFound)
		})
		r.Get("/recap", func(w http.ResponseWriter, r *http.Request) {
			render(w, r, view.RecapPlaceholder())
		})

		r.Mount("/admin", adminRoutes(d))
	})

	return r, nil
}
