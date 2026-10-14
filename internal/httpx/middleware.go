package httpx

import (
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
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

// limitByIP allows n requests per minute per client IP. It keys on RemoteAddr only:
// httprate's own IP helpers trust X-Forwarded-For, which anyone could forge when the
// app is not behind a proxy. With TRUST_PROXY=true, middleware.RealIP has already
// rewritten RemoteAddr from the proxy's header.
func limitByIP(n int) func(http.Handler) http.Handler {
	return httprate.Limit(n, time.Minute, httprate.WithKeyFuncs(func(r *http.Request) (string, error) {
		return clientIP(r), nil
	}))
}

const sessionAdminID = "admin_id"

// requireAdmin redirects to the login page unless the session holds an admin id.
// Admin pages are never cached.
func requireAdmin(sessions *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sessions.GetInt64(r.Context(), sessionAdminID) == 0 {
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}
			w.Header().Set("Cache-Control", "private, no-store")
			next.ServeHTTP(w, r)
		})
	}
}

// noStore keeps fragments and live pages out of every cache.
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

const photoUploadPath = "/patrol/tonight/photos"

// limitBodies caps every request body: 64 KB for forms, a little over 1 MB for the
// photo upload. It must run before the CSRF middleware, because nosurf parses the form
// to find the token and would otherwise read far more than that. Reading past the cap
// fails with *http.MaxBytesError, so an oversized form ends up as a rejected request.
func limitBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(formBodyLimit)
		if r.URL.Path == photoUploadPath {
			limit = photoBodyLimit
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

// securityHeaders sets the headers every response should carry.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// requestLogger writes one structured line per request. It logs the path but never
// the query string, headers, cookies or body, so passwords and session tokens stay out.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		slog.Info("request",
			"method", r.Method, "path", r.URL.Path, "status", status, "bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context()), "ip", clientIP(r))
	})
}

// clientIP is the connection's address; with TRUST_PROXY, RealIP has already replaced it
// with the address from the proxy's header.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// realIP replaces RemoteAddr with the client address a trusted reverse proxy reports.
// Only the rightmost X-Forwarded-For entry is used: it is the one the nearest proxy
// appended, so entries a client put in front of it are ignored. True-Client-IP and
// X-Real-IP (which chi's RealIP trusts first) are ignored entirely, because proxies
// such as Caddy pass them through unchanged.
func realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
				r.RemoteAddr = ip
			}
		}
		next.ServeHTTP(w, r)
	})
}
