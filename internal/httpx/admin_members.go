package httpx

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/akbarbinaji/patrolhub/internal/service"
	"github.com/akbarbinaji/patrolhub/internal/view"
)

var memberFields = []string{"house_block", "name", "phone", "notes"}

func (h adminHandlers) memberRoutes(r chi.Router) {
	r.Get("/members", h.membersPage)
	r.Post("/members", h.createMember)
	r.Get("/members/{id}/edit", h.editMemberPage)
	r.Post("/members/{id}", h.updateMember)
	r.Post("/members/{id}/active", h.setMemberActive)
}

func memberInput(r *http.Request) service.MemberInput {
	return service.MemberInput{
		HouseBlock: r.PostFormValue("house_block"), Name: r.PostFormValue("name"),
		Phone: r.PostFormValue("phone"), Notes: r.PostFormValue("notes"),
	}
}

func (h adminHandlers) renderMembers(w http.ResponseWriter, r *http.Request, status int, form view.Form) {
	members, err := h.Members.List(r.Context())
	if err != nil {
		serverError(w, "list members", err)
		return
	}
	rows := make([]view.MemberRow, len(members))
	for i, m := range members {
		rows[i] = view.MemberRow{ID: m.ID, Label: service.MemberLabel(m), Block: m.HouseBlock, Active: m.Active}
		if m.Phone != nil {
			rows[i].Phone = *m.Phone
		}
	}
	flash := h.Sessions.PopString(r.Context(), sessionFlash)
	w.WriteHeader(status)
	render(w, r, view.MembersPage(rows, form, flash))
}

func (h adminHandlers) membersPage(w http.ResponseWriter, r *http.Request) {
	h.renderMembers(w, r, http.StatusOK, view.Form{})
}

func (h adminHandlers) createMember(w http.ResponseWriter, r *http.Request) {
	if r.ParseForm() != nil {
		badRequest(w)
		return
	}
	_, err := h.Members.Create(r.Context(), memberInput(r))
	if errs, ok := validationErrors(err); ok {
		h.renderMembers(w, r, http.StatusUnprocessableEntity, view.Form{Values: formValues(r, memberFields...), Errors: errs})
		return
	}
	if err != nil {
		serverError(w, "create member", err)
		return
	}
	h.flashRedirect(w, r, "/admin/members", "Anggota ditambahkan.")
}

func (h adminHandlers) editMemberPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		notFoundPage(w)
		return
	}
	m, err := h.Members.Get(r.Context(), id)
	if errors.Is(err, service.ErrNotFound) {
		notFoundPage(w)
		return
	}
	if err != nil {
		serverError(w, "get member", err)
		return
	}
	vals := map[string]string{"house_block": m.HouseBlock}
	for k, p := range map[string]*string{"name": m.Name, "phone": m.Phone, "notes": m.Notes} {
		if p != nil {
			vals[k] = *p
		}
	}
	render(w, r, view.MemberEditPage(id, view.Form{Values: vals}))
}

func (h adminHandlers) updateMember(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok || r.ParseForm() != nil {
		badRequest(w)
		return
	}
	_, err := h.Members.Update(r.Context(), id, memberInput(r))
	if errors.Is(err, service.ErrNotFound) {
		notFoundPage(w)
		return
	}
	if errs, ok := validationErrors(err); ok {
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, view.MemberEditPage(id, view.Form{Values: formValues(r, memberFields...), Errors: errs}))
		return
	}
	if err != nil {
		serverError(w, "update member", err)
		return
	}
	h.flashRedirect(w, r, "/admin/members", "Anggota diperbarui.")
}

func (h adminHandlers) setMemberActive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok || r.ParseForm() != nil {
		badRequest(w)
		return
	}
	active, err := strconv.ParseBool(r.PostFormValue("active"))
	if err != nil {
		badRequest(w)
		return
	}
	switch err := h.Members.SetActive(r.Context(), id, active); {
	case errors.Is(err, service.ErrNotFound):
		notFoundPage(w)
	case err != nil:
		serverError(w, "set member active", err)
	default:
		msg := "Anggota dinonaktifkan."
		if active {
			msg = "Anggota diaktifkan."
		}
		h.flashRedirect(w, r, "/admin/members", msg)
	}
}
