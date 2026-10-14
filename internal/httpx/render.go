package httpx

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
)

// render writes a templ component as an HTML response.
func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		slog.Error("render failed", "path", r.URL.Path, "err", err)
	}
}
