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
	r.Post("/submit", h.submit)
	r.Post("/rain", h.rain)
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
	case errors.Is(err, service.ErrNoSituation):
		http.Error(w, "Isi kolom situasi terlebih dahulu.", http.StatusUnprocessableEntity)
	case errors.Is(err, service.ErrTooFewPhotos):
		http.Error(w, "Foto belum cukup untuk mengirim laporan.", http.StatusUnprocessableEntity)
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

// submit saves the whole report and locks it; the page then reloads into the summary.
func (h patrolHandlers) submit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		badRequest(w)
		return
	}
	in := service.SubmitInput{Note: r.PostFormValue("note")}
	for _, field := range []struct {
		name string
		ids  *[]int64
	}{{"present", &in.Present}, {"walk_in", &in.WalkIns}} {
		for _, raw := range r.PostForm[field.name] {
			id := parseID(raw)
			if id <= 0 {
				badRequest(w)
				return
			}
			*field.ids = append(*field.ids, id)
		}
	}
	if err := h.Patrol.Submit(r.Context(), h.Now(), in, reportContext(r)); err != nil {
		h.fail(w, r, err)
		return
	}
	reloadTonight(w, r)
}

func (h patrolHandlers) rain(w http.ResponseWriter, r *http.Request) {
	if err := h.Patrol.RequestRain(r.Context(), h.Now(), reportContext(r)); err != nil {
		h.fail(w, r, err)
		return
	}
	reloadTonight(w, r)
}

// reloadTonight sends the reporter back to the report page, which is now read-only.
func reloadTonight(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/patrol/tonight")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/patrol/tonight", http.StatusSeeOther)
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
