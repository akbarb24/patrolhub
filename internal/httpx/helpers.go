package httpx

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/service"
)

// flashRedirect stores a one-time message and redirects (POST-redirect-GET).
func (h adminHandlers) flashRedirect(w http.ResponseWriter, r *http.Request, to, msg string) {
	h.Sessions.Put(r.Context(), sessionFlash, msg)
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (h adminHandlers) today() domain.Date { return domain.DateOf(h.Now()) }

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return id, err == nil && id > 0
}

func notFoundPage(w http.ResponseWriter) {
	http.Error(w, "Halaman tidak ditemukan.", http.StatusNotFound)
}

func serverError(w http.ResponseWriter, what string, err error) {
	slog.Error(what, "err", err)
	http.Error(w, "Terjadi kesalahan, coba lagi.", http.StatusInternalServerError)
}

func badRequest(w http.ResponseWriter) {
	http.Error(w, "Permintaan tidak valid.", http.StatusBadRequest)
}

// validationErrors extracts per-field messages from a service error.
func validationErrors(err error) (map[string]string, bool) {
	var ve service.ValidationErrors
	if errors.As(err, &ve) {
		return ve, true
	}
	return nil, false
}

// formValues collects the trimmed values of the named fields so an invalid form can be echoed back.
func formValues(r *http.Request, names ...string) map[string]string {
	v := make(map[string]string, len(names))
	for _, n := range names {
		v[n] = strings.TrimSpace(r.PostFormValue(n))
	}
	return v
}

func parseID(s string) int64 {
	id, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return id
}
