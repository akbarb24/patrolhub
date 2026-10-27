package httpx

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/service"
	"github.com/akbarbinaji/patrolhub/internal/view"
)

func (h adminHandlers) nightRoutes(r chi.Router) {
	r.Get("/nights", h.nightsPage)
	r.Get("/nights/{date}", h.nightPage)
	r.Post("/nights/{date}/rain", h.nightAction(h.Nights.MarkRain, "Malam ditandai hujan."))
	r.Post("/nights/{date}/rain/confirm", h.nightAction(h.Nights.ConfirmRain, "Hujan dikonfirmasi."))
	r.Post("/nights/{date}/rain/reject", h.nightAction(h.Nights.RejectRain, "Pengajuan hujan ditolak."))
	r.Post("/nights/{date}/attendance/{memberID}", h.correctAttendance)
	r.Get("/photos/{id}", h.photo)
}

// parseMonth reads "YYYY-MM" and falls back to the current month.
func (h adminHandlers) parseMonth(s string) time.Time {
	if t, err := time.ParseInLocation("2006-01", s, time.UTC); err == nil {
		return t
	}
	today := h.today()
	return time.Date(today.Year, today.Month, 1, 0, 0, 0, 0, time.UTC)
}

func monthLabel(m time.Time) string {
	return domain.MonthNameID(m.Month()) + " " + strconv.Itoa(m.Year())
}

func (h adminHandlers) nightsPage(w http.ResponseWriter, r *http.Request) {
	month := h.parseMonth(r.URL.Query().Get("month"))
	onlyReview := r.URL.Query().Get("review") == "1"
	pending, rows, err := h.Nights.List(r.Context(), month.Year(), month.Month(), onlyReview)
	if err != nil {
		serverError(w, "list nights", err)
		return
	}
	flash := h.Sessions.PopString(r.Context(), sessionFlash)
	render(w, r, view.NightsPage(month.Format("2006-01"), monthLabel(month),
		month.AddDate(0, -1, 0).Format("2006-01"), month.AddDate(0, 1, 0).Format("2006-01"), onlyReview, pending, rows, flash))
}

func (h adminHandlers) renderNight(w http.ResponseWriter, r *http.Request, status int, date domain.Date, form view.Form) {
	d, err := h.Nights.Detail(r.Context(), date)
	if errors.Is(err, service.ErrNotFound) {
		notFoundPage(w)
		return
	}
	if err != nil {
		serverError(w, "night detail", err)
		return
	}
	flash := h.Sessions.PopString(r.Context(), sessionFlash)
	w.WriteHeader(status)
	render(w, r, view.NightDetailPage(d, form, flash))
}

func nightDate(r *http.Request) (domain.Date, bool) {
	d, err := domain.ParseDate(chi.URLParam(r, "date"))
	return d, err == nil
}

func (h adminHandlers) nightPage(w http.ResponseWriter, r *http.Request) {
	date, ok := nightDate(r)
	if !ok {
		notFoundPage(w)
		return
	}
	h.renderNight(w, r, http.StatusOK, date, view.Form{})
}

// nightAction adapts a service action that only needs the admin and the date.
func (h adminHandlers) nightAction(do func(ctx context.Context, adminID int64, date domain.Date) error, done string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		date, ok := nightDate(r)
		if !ok {
			notFoundPage(w)
			return
		}
		adminID := h.Sessions.GetInt64(r.Context(), sessionAdminID)
		back := "/admin/nights/" + date.String()
		switch err := do(r.Context(), adminID, date); {
		case errors.Is(err, service.ErrNotFound):
			notFoundPage(w)
		case errors.Is(err, service.ErrInvalidState):
			http.Error(w, "Tindakan ini tidak bisa dilakukan pada status malam saat ini.", http.StatusConflict)
		case err != nil:
			serverError(w, "night action", err)
		default:
			h.flashRedirect(w, r, back, done)
		}
	}
}

func (h adminHandlers) correctAttendance(w http.ResponseWriter, r *http.Request) {
	date, ok := nightDate(r)
	memberID, ok2 := pathID(r, "memberID")
	if !ok || !ok2 || r.ParseForm() != nil {
		notFoundPage(w)
		return
	}
	adminID := h.Sessions.GetInt64(r.Context(), sessionAdminID)
	err := h.Nights.CorrectAttendance(r.Context(), adminID, date, memberID, domain.AttendanceStatus(r.PostFormValue("status")), r.PostFormValue("reason"))
	switch {
	case errors.Is(err, service.ErrNotFound):
		notFoundPage(w)
	case errors.Is(err, service.ErrInvalidState):
		http.Error(w, "Koreksi hanya bisa dilakukan pada malam yang sudah ditutup dan bukan malam hujan.", http.StatusConflict)
	case err != nil:
		if errs, ok := validationErrors(err); ok {
			vals := formValues(r, "status", "reason")
			vals["member_id"] = strconv.FormatInt(memberID, 10)
			h.renderNight(w, r, http.StatusUnprocessableEntity, date, view.Form{Values: vals, Errors: errs})
			return
		}
		serverError(w, "correct attendance", err)
	default:
		h.flashRedirect(w, r, "/admin/nights/"+date.String(), "Koreksi tersimpan.")
	}
}

func (h adminHandlers) photo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		notFoundPage(w)
		return
	}
	f, contentType, err := h.Nights.OpenPhoto(r.Context(), id)
	if errors.Is(err, service.ErrNotFound) {
		notFoundPage(w)
		return
	}
	if err != nil {
		serverError(w, "open photo", err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	io.Copy(w, f)
}

// Fines

func (h adminHandlers) fineRoutes(r chi.Router) {
	r.Get("/fines", func(w http.ResponseWriter, r *http.Request) {
		t := h.today()
		http.Redirect(w, r, fmt.Sprintf("/admin/fines/%04d/%02d", t.Year, int(t.Month)), http.StatusFound)
	})
	r.Get("/fines/{yyyy}/{mm}", h.finesPage)
	r.Post("/fines/{yyyy}/{mm}/payments", h.recordPayment)
	r.Post("/fines/{yyyy}/{mm}/payments/{id}/delete", h.deletePayment)
}

func finePeriod(r *http.Request) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01", chi.URLParam(r, "yyyy")+"-"+chi.URLParam(r, "mm"), time.UTC)
	return t, err == nil
}

func (h adminHandlers) renderFines(w http.ResponseWriter, r *http.Request, status int, month time.Time, form view.Form) {
	fm, err := h.Fines.Month(r.Context(), month.Year(), month.Month())
	if err != nil {
		serverError(w, "fines", err)
		return
	}
	flash := h.Sessions.PopString(r.Context(), sessionFlash)
	w.WriteHeader(status)
	render(w, r, view.FinesPage(fm, monthLabel(month),
		month.AddDate(0, -1, 0).Format("2006/01"), month.AddDate(0, 1, 0).Format("2006/01"), h.today().String(), form, flash))
}

func (h adminHandlers) finesPage(w http.ResponseWriter, r *http.Request) {
	month, ok := finePeriod(r)
	if !ok {
		notFoundPage(w)
		return
	}
	h.renderFines(w, r, http.StatusOK, month, view.Form{})
}

func (h adminHandlers) recordPayment(w http.ResponseWriter, r *http.Request) {
	month, ok := finePeriod(r)
	if !ok || r.ParseForm() != nil {
		notFoundPage(w)
		return
	}
	vals := formValues(r, "member_id", "amount", "paid_at")
	paidAt, perr := domain.ParseDate(vals["paid_at"])
	amount, aerr := strconv.Atoi(vals["amount"])
	errs := map[string]string{}
	if perr != nil {
		errs["paid_at"] = "Tanggal harus berformat TTTT-BB-HH."
	}
	if aerr != nil {
		errs["amount"] = "Masukkan angka bulat."
	}
	if len(errs) == 0 {
		err := h.Fines.RecordPayment(r.Context(), h.Sessions.GetInt64(r.Context(), sessionAdminID), service.PaymentInput{
			MemberID: parseID(vals["member_id"]), Year: month.Year(), Month: month.Month(), Amount: amount, PaidAt: paidAt,
		})
		if e, ok := validationErrors(err); ok {
			errs = e
		} else if err != nil {
			serverError(w, "record payment", err)
			return
		} else {
			h.flashRedirect(w, r, fmt.Sprintf("/admin/fines/%04d/%02d", month.Year(), int(month.Month())), "Pembayaran dicatat.")
			return
		}
	}
	if errs["member_id"] != "" {
		errs["_"] = errs["member_id"]
	}
	h.renderFines(w, r, http.StatusUnprocessableEntity, month, view.Form{Values: vals, Errors: errs})
}

func (h adminHandlers) deletePayment(w http.ResponseWriter, r *http.Request) {
	month, ok := finePeriod(r)
	id, ok2 := pathID(r, "id")
	if !ok || !ok2 {
		notFoundPage(w)
		return
	}
	switch err := h.Fines.DeletePayment(r.Context(), h.Sessions.GetInt64(r.Context(), sessionAdminID), id); {
	case errors.Is(err, service.ErrNotFound):
		notFoundPage(w)
	case err != nil:
		serverError(w, "delete payment", err)
	default:
		h.flashRedirect(w, r, fmt.Sprintf("/admin/fines/%04d/%02d", month.Year(), int(month.Month())), "Pembayaran dihapus.")
	}
}

// CSV export (F5.5)

func (h adminHandlers) exportRoutes(r chi.Router) {
	r.Get("/export/{yyyy}/{mm}.csv", h.exportCSV)
	r.Get("/export/{yyyy}/{mm}.xlsx", h.exportXLSX)
}

// csvSafe stops spreadsheets from running a cell as a formula: a leading = + - @ or
// control character makes Excel and Sheets evaluate it, so such cells get a leading quote.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (h adminHandlers) exportCSV(w http.ResponseWriter, r *http.Request) {
	month, ok := finePeriod(r)
	if !ok {
		notFoundPage(w)
		return
	}
	rows, err := h.Recap.Export(r.Context(), month.Year(), month.Month(), h.today())
	switch {
	case errors.Is(err, service.ErrFutureMonth), errors.Is(err, service.ErrNotFound):
		notFoundPage(w)
		return
	case err != nil:
		serverError(w, "export", err)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="rekap-ronda-%04d-%02d.csv"`, month.Year(), int(month.Month())))
	io.WriteString(w, "\xEF\xBB\xBF") // UTF-8 byte order mark, so Excel reads the accents correctly
	cw := csv.NewWriter(w)
	cw.Write([]string{"tanggal", "hari", "status_malam", "blok", "nama", "status_absen", "denda"})
	for _, row := range rows {
		cw.Write([]string{
			row.Date.String(), row.Weekday, row.NightStatus, csvSafe(row.Block), csvSafe(row.Name), row.Status, strconv.Itoa(row.Fine),
		})
	}
	cw.Flush()
}
