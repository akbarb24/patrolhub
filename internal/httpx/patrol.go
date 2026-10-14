package httpx

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/akbarbinaji/patrolhub/internal/service"
	"github.com/akbarbinaji/patrolhub/internal/storage"
	"github.com/akbarbinaji/patrolhub/internal/view"
)

const (
	formBodyLimit  = 64 << 10
	photoBodyLimit = service.MaxPhotoBytes + 64<<10 // the photo plus multipart overhead
)

type patrolHandlers struct {
	Deps
}

func patrolRoutes(d Deps) http.Handler {
	h := patrolHandlers{d}
	r := chi.NewRouter()
	r.Use(limitByIP(60), noStore)

	r.Get("/", h.page)
	r.Post("/attendance/{memberID}", h.toggle)
	r.Post("/situation", h.situation)
	r.Post("/rain", h.rain)
	r.Post("/walk-in", h.walkIn)
	r.Post("/photos", h.addPhoto)
	r.Get("/photos/{id}/thumb", h.thumb)
	return r
}

func reportContext(r *http.Request) service.ReportContext {
	return service.ReportContext{IP: clientIP(r), UserAgent: r.UserAgent()}
}

// fail maps service errors to a status and a short Indonesian message the page shows as a toast.
func (h patrolHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.Is(err, service.ErrWindowClosed), errors.Is(err, service.ErrNightClosed):
		// htmx follows HX-Redirect even on an error status, which reloads the page into its closed state.
		w.Header().Set("HX-Redirect", "/patrol/tonight")
		http.Error(w, "Laporan sudah ditutup.", http.StatusConflict)
	case errors.Is(err, service.ErrNotInTeam):
		http.Error(w, "Anggota tidak ada di tim malam ini.", http.StatusBadRequest)
	case errors.Is(err, service.ErrAlreadyInTeam):
		http.Error(w, "Anggota sudah ada di tim.", http.StatusConflict)
	case errors.Is(err, service.ErrTooLong):
		http.Error(w, "Situasi maksimal 1.000 karakter.", http.StatusUnprocessableEntity)
	case errors.Is(err, service.ErrTooManyPhotos):
		http.Error(w, "Jumlah foto sudah mencapai batas.", http.StatusUnprocessableEntity)
	case errors.Is(err, service.ErrPhotoTooLarge), errors.As(err, &tooBig):
		http.Error(w, "Foto terlalu besar (maksimal 1 MB).", http.StatusRequestEntityTooLarge)
	case errors.Is(err, service.ErrBadImageType):
		http.Error(w, "Format foto tidak didukung (gunakan JPEG atau WebP).", http.StatusUnsupportedMediaType)
	default:
		slog.Error("patrol request failed", "path", r.URL.Path, "err", err)
		http.Error(w, "Gagal menyimpan, coba lagi.", http.StatusInternalServerError)
	}
}

func (h patrolHandlers) page(w http.ResponseWriter, r *http.Request) {
	v, err := h.Patrol.Tonight(r.Context(), h.Now())
	if err != nil {
		serverError(w, "tonight", err)
		return
	}
	render(w, r, view.TonightPage(v))
}

func (h patrolHandlers) toggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "memberID")
	present, err := strconv.ParseBool(r.PostFormValue("present"))
	if !ok || err != nil {
		badRequest(w)
		return
	}
	row, err := h.Patrol.ToggleAttendance(r.Context(), h.Now(), id, present, reportContext(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	render(w, r, view.TeamRowItem(row))
}

func (h patrolHandlers) situation(w http.ResponseWriter, r *http.Request) {
	now := h.Now()
	if err := h.Patrol.UpdateSituation(r.Context(), now, r.PostFormValue("note"), reportContext(r)); err != nil {
		h.fail(w, r, err)
		return
	}
	render(w, r, view.SituationSaved(view.ClockText(now)))
}

func (h patrolHandlers) rain(w http.ResponseWriter, r *http.Request) {
	now := h.Now()
	if err := h.Patrol.RequestRain(r.Context(), now, reportContext(r)); err != nil {
		h.fail(w, r, err)
		return
	}
	v, err := h.Patrol.Tonight(r.Context(), now)
	if err != nil {
		serverError(w, "tonight", err)
		return
	}
	render(w, r, view.RainPanel(v))
}

func (h patrolHandlers) walkIn(w http.ResponseWriter, r *http.Request) {
	now := h.Now()
	if err := h.Patrol.AddWalkIn(r.Context(), now, parseID(r.PostFormValue("member_id")), reportContext(r)); err != nil {
		h.fail(w, r, err)
		return
	}
	v, err := h.Patrol.Tonight(r.Context(), now)
	if err != nil {
		serverError(w, "tonight", err)
		return
	}
	render(w, r, view.TeamSection(v))
}

func optionalFloat(s string) *float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &f
}

func (h patrolHandlers) addPhoto(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		h.fail(w, r, err)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("photo")
	if err != nil {
		badRequest(w)
		return
	}
	defer file.Close()

	now := h.Now()
	_, err = h.Patrol.AddPhoto(r.Context(), now, file, header.Size,
		optionalFloat(r.FormValue("lat")), optionalFloat(r.FormValue("lng")), reportContext(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	v, err := h.Patrol.Tonight(r.Context(), now)
	if err != nil {
		serverError(w, "tonight", err)
		return
	}
	render(w, r, view.PhotoPanel(v))
}

func (h patrolHandlers) thumb(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		notFoundPage(w)
		return
	}
	f, contentType, err := h.Patrol.OpenTonightPhoto(r.Context(), h.Now(), id)
	if errors.Is(err, service.ErrNotFound) || errors.Is(err, storage.ErrInvalidKey) {
		notFoundPage(w)
		return
	}
	if err != nil {
		serverError(w, "open photo", err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentType)
	io.Copy(w, f)
}
