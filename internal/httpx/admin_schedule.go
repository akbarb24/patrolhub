package httpx

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/service"
	"github.com/akbarbinaji/patrolhub/internal/view"
)

func (h adminHandlers) scheduleRoutes(r chi.Router) {
	r.Get("/schedule", h.schedulePage)
	r.Post("/schedule/rules", h.addRule)
	r.Post("/schedule/rules/{id}/end", h.endRule)
	r.Get("/schedule/overrides", h.overridesPage)
	r.Get("/schedule/overrides/preview", h.teamPreview)
	r.Post("/schedule/overrides", h.createOverride)
	r.Post("/schedule/overrides/{id}/delete", h.deleteOverride)
	r.Get("/schedule/preview", h.monthPreview)
}

func dayOptions() []view.Option {
	opts := make([]view.Option, 7)
	for i := range opts {
		opts[i] = view.Option{Value: strconv.Itoa(i + 1), Label: domain.WeekdayNameID(i + 1)}
	}
	return opts
}

func (h adminHandlers) memberOptions(r *http.Request) ([]view.Option, error) {
	members, err := h.Members.ListActive(r.Context())
	if err != nil {
		return nil, err
	}
	opts := make([]view.Option, len(members))
	for i, m := range members {
		opts[i] = view.Option{Value: strconv.FormatInt(m.ID, 10), Label: service.MemberLabel(m)}
	}
	return opts, nil
}

func (h adminHandlers) renderSchedule(w http.ResponseWriter, r *http.Request, status int, form view.Form) {
	today := h.today()
	week, err := h.Schedule.Week(r.Context(), today)
	if err != nil {
		serverError(w, "schedule week", err)
		return
	}
	members, err := h.memberOptions(r)
	if err != nil {
		serverError(w, "member options", err)
		return
	}
	if form.Values == nil {
		form.Values = map[string]string{}
	}
	if form.Values["effective_from"] == "" {
		form.Values["effective_from"] = today.String()
	}
	flash := h.Sessions.PopString(r.Context(), sessionFlash)
	w.WriteHeader(status)
	render(w, r, view.SchedulePage(week, members, dayOptions(), form, flash))
}

func (h adminHandlers) schedulePage(w http.ResponseWriter, r *http.Request) {
	h.renderSchedule(w, r, http.StatusOK, view.Form{})
}

func (h adminHandlers) addRule(w http.ResponseWriter, r *http.Request) {
	if r.ParseForm() != nil {
		badRequest(w)
		return
	}
	var err error
	from, perr := domain.ParseDate(r.PostFormValue("effective_from"))
	if perr != nil {
		err = service.ValidationErrors{"effective_from": "Tanggal harus berformat TTTT-BB-HH."}
	} else {
		dow, _ := strconv.Atoi(r.PostFormValue("day_of_week"))
		err = h.Schedule.AddRule(r.Context(), parseID(r.PostFormValue("member_id")), dow, from)
	}
	if errs, ok := validationErrors(err); ok {
		h.renderSchedule(w, r, http.StatusUnprocessableEntity,
			view.Form{Values: formValues(r, "member_id", "day_of_week", "effective_from"), Errors: errs})
		return
	}
	if err != nil {
		serverError(w, "add rule", err)
		return
	}
	h.flashRedirect(w, r, "/admin/schedule", "Peserta ditambahkan ke jadwal.")
}

func (h adminHandlers) endRule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		notFoundPage(w)
		return
	}
	switch err := h.Schedule.EndRule(r.Context(), id, h.today()); {
	case errors.Is(err, service.ErrNotFound):
		notFoundPage(w)
	case err != nil:
		serverError(w, "end rule", err)
	default:
		h.flashRedirect(w, r, "/admin/schedule", "Peserta dikeluarkan dari jadwal.")
	}
}

// Overrides

func overrideActions() []view.Option {
	return []view.Option{{Value: "ADD", Label: "Tambah"}, {Value: "REMOVE", Label: "Keluarkan"}, {Value: "SWAP", Label: "Tukar"}}
}

func (h adminHandlers) renderOverrides(w http.ResponseWriter, r *http.Request, status int, date domain.Date, form view.Form) {
	team, err := h.Schedule.Team(r.Context(), date)
	if err != nil {
		serverError(w, "team preview", err)
		return
	}
	list, err := h.Schedule.Overrides(r.Context(), h.today())
	if err != nil {
		serverError(w, "list overrides", err)
		return
	}
	members, err := h.memberOptions(r)
	if err != nil {
		serverError(w, "member options", err)
		return
	}
	flash := h.Sessions.PopString(r.Context(), sessionFlash)
	w.WriteHeader(status)
	render(w, r, view.OverridesPage(date, team, list, members, overrideActions(), form, flash))
}

func (h adminHandlers) overridesPage(w http.ResponseWriter, r *http.Request) {
	date, err := domain.ParseDate(r.URL.Query().Get("date"))
	if err != nil {
		date = h.today()
	}
	h.renderOverrides(w, r, http.StatusOK, date, view.Form{})
}

func (h adminHandlers) teamPreview(w http.ResponseWriter, r *http.Request) {
	date, err := domain.ParseDate(r.URL.Query().Get("patrol_date"))
	if err != nil {
		date = h.today()
	}
	team, err := h.Schedule.Team(r.Context(), date)
	if err != nil {
		serverError(w, "team preview", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	render(w, r, view.TeamPreview(date, team))
}

func (h adminHandlers) createOverride(w http.ResponseWriter, r *http.Request) {
	if r.ParseForm() != nil {
		badRequest(w)
		return
	}
	values := formValues(r, "patrol_date", "action", "member_id", "replacement_member_id", "note")
	date, perr := domain.ParseDate(values["patrol_date"])
	if perr != nil {
		h.renderOverrides(w, r, http.StatusUnprocessableEntity, h.today(), view.Form{
			Values: values, Errors: map[string]string{"patrol_date": "Tanggal harus berformat TTTT-BB-HH."}})
		return
	}
	in := service.OverrideInput{Date: date, Action: values["action"], MemberID: parseID(values["member_id"]), Note: values["note"]}
	if id := parseID(values["replacement_member_id"]); id != 0 {
		in.ReplacementID = &id
	}
	err := h.Schedule.CreateOverride(r.Context(), in)
	if errs, ok := validationErrors(err); ok {
		h.renderOverrides(w, r, http.StatusUnprocessableEntity, date, view.Form{Values: values, Errors: errs})
		return
	}
	if err != nil {
		serverError(w, "create override", err)
		return
	}
	h.flashRedirect(w, r, "/admin/schedule/overrides?date="+date.String(), "Perubahan jadwal disimpan.")
}

func (h adminHandlers) deleteOverride(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		notFoundPage(w)
		return
	}
	switch err := h.Schedule.DeleteOverride(r.Context(), id); {
	case errors.Is(err, service.ErrNotFound):
		notFoundPage(w)
	case errors.Is(err, service.ErrNightClosed):
		http.Error(w, "Malam itu sudah ditutup; perubahan tidak bisa dihapus.", http.StatusConflict)
	case err != nil:
		serverError(w, "delete override", err)
	default:
		h.flashRedirect(w, r, "/admin/schedule/overrides", "Perubahan dihapus.")
	}
}

// Month preview (F1.5)

func (h adminHandlers) monthPreview(w http.ResponseWriter, r *http.Request) {
	month, err := time.ParseInLocation("2006-01", r.URL.Query().Get("month"), time.UTC)
	if err != nil {
		t := h.today()
		month = time.Date(t.Year, t.Month, 1, 0, 0, 0, 0, time.UTC)
	}
	days, err := h.Schedule.MonthPreview(r.Context(), month.Year(), month.Month())
	if err != nil {
		serverError(w, "month preview", err)
		return
	}
	label := domain.MonthNameID(month.Month()) + " " + strconv.Itoa(month.Year())
	prev, next := month.AddDate(0, -1, 0).Format("2006-01"), month.AddDate(0, 1, 0).Format("2006-01")
	render(w, r, view.SchedulePreviewPage(label, prev, next, days))
}
