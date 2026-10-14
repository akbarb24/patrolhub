package httpx

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestMembersCRUDFlow(t *testing.T) {
	a := newApp(t, wibTime(2026, 10, 7, 12, 0))
	b := a.loggedIn()

	// A member with only a block shows up as the block, upper-cased.
	b.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {" c-10 "}}, http.StatusSeeOther)
	b.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {"a-2"}, "name": {"Dhani"}, "phone": {"0812"}}, http.StatusSeeOther)
	body := b.mustGet("/admin/members")
	contains(t, body, "Dhani (A-2)", ">C-10<", "Anggota ditambahkan.", "0812")
	notContains(t, body, "c-10")

	// Blank block is rejected and echoed back with an error under the field.
	body = b.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {" "}, "name": {"Tanpa Blok"}}, http.StatusUnprocessableEntity)
	contains(t, body, "Blok rumah wajib diisi.", `id="house_block-error"`, `value="Tanpa Blok"`)

	// Edit.
	body = b.mustGet("/admin/members/2/edit")
	contains(t, body, `value="A-2"`, `value="Dhani"`)
	b.mustPost("/admin/members/2/edit", "/admin/members/2", url.Values{"house_block": {"a-3"}, "name": {"Dhani"}}, http.StatusSeeOther)
	contains(t, b.mustGet("/admin/members"), "Dhani (A-3)")
	if resp, _ := b.do("GET", "/admin/members/999/edit", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown member edit: %d", resp.StatusCode)
	}

	// Deactivate / reactivate: never deleted.
	b.mustPost("/admin/members", "/admin/members/1/active", url.Values{"active": {"false"}}, http.StatusSeeOther)
	contains(t, b.mustGet("/admin/members"), ">Nonaktif</span>", "C-10")
	b.mustPost("/admin/members", "/admin/members/1/active", url.Values{"active": {"true"}}, http.StatusSeeOther)
	notContains(t, b.mustGet("/admin/members"), ">Nonaktif</span>")
	if resp, _ := b.post("/admin/members", "/admin/members/1/active", url.Values{"active": {"maybe"}}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad active value: %d", resp.StatusCode)
	}
}

func TestScheduleOverridesAndPreviewFlow(t *testing.T) {
	a := newApp(t, wibTime(2026, 10, 7, 12, 0)) // Wednesday
	b := a.loggedIn()
	b.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {"A-1"}, "name": {"Anton"}}, http.StatusSeeOther)
	b.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {"B-2"}, "name": {"Budi"}}, http.StatusSeeOther)
	b.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {"C-3"}}, http.StatusSeeOther)

	// Fixed Monday duty for Anton and Budi.
	for _, id := range []string{"1", "2"} {
		b.mustPost("/admin/schedule", "/admin/schedule/rules",
			url.Values{"member_id": {id}, "day_of_week": {"1"}, "effective_from": {"2026-01-01"}}, http.StatusSeeOther)
	}
	body := b.mustGet("/admin/schedule")
	contains(t, body, "Anton (A-1)", "Budi (B-2)", "Senin")

	// Duplicate and invalid input are shown under the field.
	body = b.mustPost("/admin/schedule", "/admin/schedule/rules",
		url.Values{"member_id": {"1"}, "day_of_week": {"1"}, "effective_from": {"2026-01-01"}}, http.StatusUnprocessableEntity)
	contains(t, body, "Peserta sudah terjadwal di hari itu.")
	body = b.mustPost("/admin/schedule", "/admin/schedule/rules",
		url.Values{"member_id": {"1"}, "day_of_week": {"2"}, "effective_from": {"besok"}}, http.StatusUnprocessableEntity)
	contains(t, body, "Tanggal harus berformat")

	// Swap Budi for C-3 next Monday: the preview shows it, other Mondays are untouched.
	b.mustPost("/admin/schedule/overrides", "/admin/schedule/overrides", url.Values{
		"patrol_date": {"2026-10-12"}, "action": {"SWAP"}, "member_id": {"2"}, "replacement_member_id": {"3"}, "note": {"sakit"},
	}, http.StatusSeeOther)
	body = b.mustGet("/admin/schedule/overrides?date=2026-10-12")
	contains(t, body, "Tukar Budi (B-2) → C-3", "sakit", "Perubahan jadwal disimpan.")
	frag := b.mustGet("/admin/schedule/overrides/preview?patrol_date=2026-10-12")
	contains(t, frag, "C-3", "Anton (A-1)", "perubahan")
	notContains(t, frag, "Budi")
	frag = b.mustGet("/admin/schedule/overrides/preview?patrol_date=2026-10-19")
	contains(t, frag, "Budi (B-2)")

	body = b.mustPost("/admin/schedule/overrides", "/admin/schedule/overrides",
		url.Values{"patrol_date": {"2026-10-12"}, "action": {"SWAP"}, "member_id": {"2"}}, http.StatusUnprocessableEntity)
	contains(t, body, "Pilih peserta pengganti.")

	// Month preview.
	body = b.mustGet("/admin/schedule/preview?month=2026-10")
	contains(t, body, "Oktober 2026", "Senin, 5 Oktober 2026", "Anton (A-1), Budi (B-2)", "Anton (A-1), C-3")

	// Delete the override while the night is open.
	b.mustPost("/admin/schedule/overrides", "/admin/schedule/overrides/1/delete", url.Values{}, http.StatusSeeOther)
	notContains(t, b.mustGet("/admin/schedule/overrides"), "Tukar Budi")

	// Taking Anton off the schedule keeps history: past Mondays still list him.
	b.mustPost("/admin/schedule", "/admin/schedule/rules/1/end", url.Values{}, http.StatusSeeOther)
	body = b.mustGet("/admin/schedule/preview?month=2026-10")
	contains(t, body, "Senin, 5 Oktober 2026")
	if !strings.Contains(body, "Anton (A-1), Budi (B-2)") {
		t.Error("history lost: 5 Oct no longer lists Anton")
	}
}

func TestMemberAndScheduleRequireLogin(t *testing.T) {
	a := newApp(t, wibTime(2026, 10, 7, 12, 0))
	b := a.anonymous()
	for _, p := range []string{"/admin/members", "/admin/members/1/edit", "/admin/schedule", "/admin/schedule/overrides", "/admin/schedule/preview"} {
		if resp, _ := b.do("GET", p, nil); resp.StatusCode != http.StatusSeeOther {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}
}
