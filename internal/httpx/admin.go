package httpx

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/skip2/go-qrcode"

	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/service"
	"github.com/akbarbinaji/patrolhub/internal/view"
)

const sessionFlash = "flash"

type adminHandlers struct {
	Deps
}

func adminRoutes(d Deps) http.Handler {
	h := adminHandlers{d}
	r := chi.NewRouter()

	r.Group(func(r chi.Router) {
		r.Use(limitByIP(10))
		r.Get("/login", h.loginForm)
		r.Post("/login", h.login)
	})
	r.Post("/logout", h.logout)

	r.Group(func(r chi.Router) {
		r.Use(requireAdmin(d.Sessions))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/admin/nights", http.StatusFound)
		})
		r.Get("/settings", h.settingsForm)
		r.Post("/settings", h.saveSettings)
		r.Get("/qr", h.qrPage)
		r.Get("/qr.png", h.qrPNG)
		h.memberRoutes(r)
		h.scheduleRoutes(r)
		h.nightRoutes(r)
		h.fineRoutes(r)
	})
	return r
}

func (h adminHandlers) loggedIn(r *http.Request) bool {
	return h.Sessions.GetInt64(r.Context(), sessionAdminID) != 0
}

func (h adminHandlers) loginForm(w http.ResponseWriter, r *http.Request) {
	if h.loggedIn(r) {
		http.Redirect(w, r, "/admin/nights", http.StatusSeeOther)
		return
	}
	render(w, r, view.LoginPage("", ""))
}

func (h adminHandlers) login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Permintaan tidak valid.", http.StatusBadRequest)
		return
	}
	username := r.PostFormValue("username")
	id, err := h.Admins.Authenticate(r.Context(), username, r.PostFormValue("password"))
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		w.WriteHeader(http.StatusUnauthorized)
		render(w, r, view.LoginPage(username, "Username atau password salah."))
		return
	case err != nil:
		slog.Error("admin login", "err", err)
		http.Error(w, "Terjadi kesalahan, coba lagi.", http.StatusInternalServerError)
		return
	}
	// A new token on privilege change prevents session fixation.
	if err := h.Sessions.RenewToken(r.Context()); err != nil {
		slog.Error("renew session token", "err", err)
		http.Error(w, "Terjadi kesalahan, coba lagi.", http.StatusInternalServerError)
		return
	}
	h.Sessions.Put(r.Context(), sessionAdminID, id)
	http.Redirect(w, r, "/admin/nights", http.StatusSeeOther)
}

func (h adminHandlers) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Sessions.Destroy(r.Context()); err != nil {
		slog.Error("destroy session", "err", err)
	}
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// Settings

func settingsFormValues(s service.Settings) map[string]string {
	live := ""
	if s.GoLiveDate != nil {
		live = s.GoLiveDate.String()
	}
	return map[string]string{
		"rt_name":          s.RTName,
		"window_open":      s.WindowOpen.String(),
		"window_close":     s.WindowClose.String(),
		"fine_per_absence": strconv.Itoa(s.FinePerAbsence),
		"min_photos":       strconv.Itoa(s.MinPhotos),
		"max_photos":       strconv.Itoa(s.MaxPhotos),
		"go_live_date":     live,
	}
}

func (h adminHandlers) settingsForm(w http.ResponseWriter, r *http.Request) {
	s, err := h.Settings.Load(r.Context())
	if err != nil {
		slog.Error("load settings", "err", err)
		http.Error(w, "Gagal memuat pengaturan.", http.StatusInternalServerError)
		return
	}
	flash := h.Sessions.PopString(r.Context(), sessionFlash)
	render(w, r, view.SettingsPage(view.Form{Values: settingsFormValues(s)}, flash))
}

// parseForm turns the submitted strings into Settings. Fields it cannot
// parse get an error message; the rest of the validation lives in Settings.Validate.
func parseForm(r *http.Request) (service.Settings, map[string]string) {
	errs := map[string]string{}
	val := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	clock := func(k, msg string) domain.Clock {
		c, err := domain.ParseClock(val(k))
		if err != nil {
			errs[k] = msg
		}
		return c
	}
	num := func(k string) int {
		n, err := strconv.Atoi(val(k))
		if err != nil {
			errs[k] = "Masukkan angka bulat."
		}
		return n
	}

	s := service.Settings{
		RTName:         val("rt_name"),
		WindowOpen:     clock("window_open", "Jam buka harus berformat HH:MM."),
		WindowClose:    clock("window_close", "Jam tutup harus berformat HH:MM."),
		FinePerAbsence: num("fine_per_absence"),
		MinPhotos:      num("min_photos"),
		MaxPhotos:      num("max_photos"),
	}
	if live := val("go_live_date"); live != "" {
		d, err := domain.ParseDate(live)
		if err != nil {
			errs["go_live_date"] = "Tanggal harus berformat TTTT-BB-HH."
		}
		s.GoLiveDate = &d
	}
	return s, errs
}

func (h adminHandlers) saveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Permintaan tidak valid.", http.StatusBadRequest)
		return
	}
	s, errs := parseForm(r)
	if len(errs) == 0 { // validating half-parsed values would only add misleading errors
		err := h.Settings.Save(r.Context(), s)
		var ve service.ValidationErrors
		switch {
		case err == nil:
			h.Sessions.Put(r.Context(), sessionFlash, "Pengaturan tersimpan.")
			http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
			return
		case errors.As(err, &ve):
			errs = ve
		default:
			slog.Error("save settings", "err", err)
			http.Error(w, "Gagal menyimpan pengaturan.", http.StatusInternalServerError)
			return
		}
	}

	values := map[string]string{}
	for k := range settingsFormValues(service.Settings{}) {
		values[k] = strings.TrimSpace(r.PostFormValue(k))
	}
	w.WriteHeader(http.StatusUnprocessableEntity)
	render(w, r, view.SettingsPage(view.Form{Values: values, Errors: errs}, ""))
}

// QR

func (h adminHandlers) qrPage(w http.ResponseWriter, r *http.Request) {
	s, err := h.Settings.Load(r.Context())
	if err != nil {
		slog.Error("load settings", "err", err)
		http.Error(w, "Gagal memuat pengaturan.", http.StatusInternalServerError)
		return
	}
	window := fmt.Sprintf("%02d.%02d–%02d.%02d", s.WindowOpen.Hour, s.WindowOpen.Min, s.WindowClose.Hour, s.WindowClose.Min)
	render(w, r, view.QRPage(s.RTName, window, h.Config.TonightURL()))
}

func (h adminHandlers) qrPNG(w http.ResponseWriter, r *http.Request) {
	png, err := qrcode.Encode(h.Config.TonightURL(), qrcode.Medium, 512)
	if err != nil {
		slog.Error("encode qr", "err", err)
		http.Error(w, "Gagal membuat QR.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}
