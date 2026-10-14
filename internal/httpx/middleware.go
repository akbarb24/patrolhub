package httpx

import (
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/justinas/nosurf"

	"github.com/akbarbinaji/patrolhub/internal/view"
)

// csrf protects every unsafe request. The token is accepted from the X-CSRF-Token
// header (HTMX, see the layout's hx-headers) or the csrf_token form field.
//
// nosurf also demands proof of same origin (Sec-Fetch-Site, Origin or Referer).
// Behind a TLS-terminating proxy the app sees plain http, so browsers that send
// Origin instead of Sec-Fetch-Site would be rejected; baseOrigin is therefore trusted.
func csrf(secure bool, baseOrigin string) (func(http.Handler) http.Handler, error) {
	allowed, err := nosurf.StaticOrigins(baseOrigin)
	if err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		h := nosurf.New(next)
		h.SetIsAllowedOriginFunc(allowed)
		h.SetBaseCookie(http.Cookie{
			Path:     "/",
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})
		h.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Permintaan ditolak: token CSRF tidak valid.", http.StatusForbidden)
		}))
		return h
	}, nil
}

// pageData puts the CSRF token and RT name into the request context for the layouts.
// A settings failure must not take the page down, so the name is then left empty.
func pageData(settings SettingsLoader) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := view.PageData{CSRFToken: nosurf.Token(r)}
			if s, err := settings.Load(r.Context()); err != nil {
				slog.Warn("load settings for page", "err", err)
			} else {
				p.RTName = s.RTName
			}
			next.ServeHTTP(w, r.WithContext(view.WithPage(r.Context(), p)))
		})
	}
}

// fingerprinted matches build-hashed names like app.3f9a1c2b.css.
var fingerprinted = regexp.MustCompile(`\.[0-9a-f]{8,}\.[a-z0-9]+$`)

// cacheStatic sets long caching for fingerprinted files and one hour for the rest.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fingerprinted.MatchString(r.URL.Path) && !strings.HasSuffix(r.URL.Path, "/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		next.ServeHTTP(w, r)
	})
}
