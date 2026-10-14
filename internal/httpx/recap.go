package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/service"
	"github.com/akbarbinaji/patrolhub/internal/view"
)

type recapHandlers struct {
	Deps
}

func recapRoutes(d Deps) http.Handler {
	h := recapHandlers{d}
	r := chi.NewRouter()
	r.Get("/", h.current)
	r.Get("/{yyyy}/{mm}", h.summary)
	r.Get("/{yyyy}/{mm}/situations", h.situations)
	return r
}

func (h recapHandlers) current(w http.ResponseWriter, r *http.Request) {
	t := domain.DateOf(h.Now())
	http.Redirect(w, r, fmt.Sprintf("/recap/%04d/%02d", t.Year, int(t.Month)), http.StatusFound)
}

// month parses /recap/{yyyy}/{mm}; ok is false for anything that is not a real month.
func recapMonth(r *http.Request) (year int, month time.Month, ok bool) {
	y, err1 := strconv.Atoi(chi.URLParam(r, "yyyy"))
	m, err2 := strconv.Atoi(chi.URLParam(r, "mm"))
	if err1 != nil || err2 != nil || len(chi.URLParam(r, "yyyy")) != 4 || len(chi.URLParam(r, "mm")) != 2 || m < 1 || m > 12 {
		return 0, 0, false
	}
	return y, time.Month(m), true
}

func recapNav(year int, month time.Month, today domain.Date) view.RecapNav {
	cur := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	href := func(t time.Time) string { return fmt.Sprintf("/recap/%04d/%02d", t.Year(), int(t.Month())) }
	nav := view.RecapNav{
		Title:   domain.MonthNameID(month) + " " + strconv.Itoa(year),
		Prev:    href(cur.AddDate(0, -1, 0)),
		Summary: href(cur), Situation: href(cur) + "/situations",
	}
	if next := cur.AddDate(0, 1, 0); next.Before(time.Date(today.Year, today.Month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)) {
		nav.Next = href(next)
	}
	return nav
}

// load fetches the recap or writes the 404/500 itself.
func (h recapHandlers) load(w http.ResponseWriter, r *http.Request) (service.Recap, view.RecapNav, bool) {
	year, month, ok := recapMonth(r)
	if !ok {
		notFoundPage(w)
		return service.Recap{}, view.RecapNav{}, false
	}
	today := domain.DateOf(h.Now())
	rec, err := h.Recap.Month(r.Context(), year, month, today)
	switch {
	case errors.Is(err, service.ErrFutureMonth), errors.Is(err, service.ErrNotFound):
		notFoundPage(w)
		return rec, view.RecapNav{}, false
	case err != nil:
		serverError(w, "recap", err)
		return rec, view.RecapNav{}, false
	}
	// The recap has no per-user content, but it carries a CSRF cookie, so keep it out of shared caches.
	w.Header().Set("Cache-Control", "private, max-age=300")
	return rec, recapNav(year, month, today), true
}

func (h recapHandlers) summary(w http.ResponseWriter, r *http.Request) {
	if rec, nav, ok := h.load(w, r); ok {
		render(w, r, view.RecapSummaryPage(rec, nav))
	}
}

func (h recapHandlers) situations(w http.ResponseWriter, r *http.Request) {
	if rec, nav, ok := h.load(w, r); ok {
		render(w, r, view.RecapSituationsPage(rec, nav, r.URL.Query().Get("special") == "1"))
	}
}
