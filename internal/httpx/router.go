// Package httpx contains the HTTP layer: router, middleware and handlers.
// Handlers talk to services, never to the database directly.
package httpx

import (
	"context"
	"io/fs"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/akbarbinaji/patrolhub/internal/config"
	"github.com/akbarbinaji/patrolhub/internal/service"
	"github.com/akbarbinaji/patrolhub/internal/view"
)

// SettingsLoader is the part of the settings service the HTTP layer needs.
type SettingsLoader interface {
	Load(ctx context.Context) (service.Settings, error)
}

type Deps struct {
	Config   config.Config
	Sessions *scs.SessionManager
	Settings SettingsLoader
	Static   fs.FS // root of the embedded static assets
}

func NewRouter(d Deps) (http.Handler, error) {
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
	})

	return r, nil
}
