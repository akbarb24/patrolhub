package httpx

import (
	"errors"
	"io"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/akbarbinaji/patrolhub/internal/service"
	"github.com/akbarbinaji/patrolhub/internal/view"
)

type sharedHandlers struct {
	Deps
}

// sharedRoutes serves a night's photos behind the secret link in the WhatsApp report.
// Anyone with the link sees them; the token is the only protection.
func sharedRoutes(d Deps) http.Handler {
	h := sharedHandlers{d}
	r := chi.NewRouter()
	r.Use(limitByIP(60), noIndex)
	r.Get("/{token}", h.page)
	r.Get("/{token}/photos/{id}", h.photo)
	return r
}

func noIndex(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		next.ServeHTTP(w, r)
	})
}

func (h sharedHandlers) page(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	date, ids, err := h.Nights.SharedPhotos(r.Context(), token)
	if errors.Is(err, service.ErrNotFound) {
		notFoundPage(w)
		return
	}
	if err != nil {
		serverError(w, "shared photos", err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	render(w, r, view.SharedPhotosPage(token, date, ids))
}

func (h sharedHandlers) photo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		notFoundPage(w)
		return
	}
	f, contentType, err := h.Nights.OpenSharedPhoto(r.Context(), chi.URLParam(r, "token"), id)
	// A missing file means the retention job already deleted it.
	if errors.Is(err, service.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		notFoundPage(w)
		return
	}
	if err != nil {
		serverError(w, "open shared photo", err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	io.Copy(w, f)
}
