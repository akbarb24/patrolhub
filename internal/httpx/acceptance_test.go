package httpx

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/service"
)

// utc builds an instant from Jakarta wall-clock time but hands it over in UTC, as a
// server in another time zone would see it.
func utc(y int, m time.Month, d, h, min int) time.Time { return wibTime(y, m, d, h, min).UTC() }

func settingsForm(open, closeAt string, fine int) url.Values {
	return url.Values{
		"rt_name": {"RT 02"}, "window_open": {open}, "window_close": {closeAt}, "fine_per_absence": {strconv.Itoa(fine)},
		"min_photos": {"3"}, "max_photos": {"10"}, "go_live_date": {"2026-10-12"},
	}
}

// TestAcceptanceChecklist walks the acceptance table of the implementation plan (step 14)
// through the real HTTP layer, with the clock handed over in UTC.
func TestAcceptanceChecklist(t *testing.T) {
	a := newApp(t, utc(2026, 10, 5, 12, 0))
	admin := a.loggedIn()
	closing := service.NewClosingService(a.pool, service.NewSettingsService(a.pool))
	tick := func(at time.Time) {
		t.Helper()
		a.now = at
		if _, err := closing.Tick(context.Background(), at); err != nil {
			t.Fatal(err)
		}
	}
	phone := a.anonymous()

	admin.mustPost("/admin/settings", "/admin/settings", url.Values{
		"rt_name": {"RT 02"}, "window_open": {"22:00"}, "window_close": {"06:00"}, "fine_per_absence": {"5000"},
		"min_photos": {"3"}, "max_photos": {"10"}, "go_live_date": {"2026-10-12"},
	}, http.StatusSeeOther)

	// ---- F1.1: a member without a name shows as the block; inactive members leave new teams only.
	t.Run("F1.1 members", func(t *testing.T) {
		admin.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {"c-10"}}, http.StatusSeeOther)
		for _, m := range [][2]string{{"A-1", "Anton"}, {"B-2", "Budi"}, {"C-1", "Citra"}, {"D-4", "Dewi"}} {
			admin.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {m[0]}, "name": {m[1]}}, http.StatusSeeOther)
		}
		contains(t, admin.mustGet("/admin/members"), ">C-10<", "Anton (A-1)")
		admin.mustPost("/admin/members", "/admin/members/5/active", url.Values{"active": {"false"}}, http.StatusSeeOther)
		contains(t, admin.mustGet("/admin/members"), ">Nonaktif</span>")
	})

	// ---- F1.2-F1.4: fixed Monday team (C-10, Anton, Budi) plus a swap for one Monday.
	t.Run("F1.2-F1.4 schedule and swap", func(t *testing.T) {
		for _, id := range []string{"1", "2", "3"} {
			admin.mustPost("/admin/schedule", "/admin/schedule/rules",
				url.Values{"member_id": {id}, "day_of_week": {"1"}, "effective_from": {"2026-01-01"}}, http.StatusSeeOther)
		}
		admin.mustPost("/admin/schedule/overrides", "/admin/schedule/overrides", url.Values{
			"patrol_date": {"2026-10-12"}, "action": {"SWAP"}, "member_id": {"3"}, "replacement_member_id": {"4"},
		}, http.StatusSeeOther)
	})

	// ---- F2.1-F2.3: the window opens 5 minutes after "now" and the page follows the clock.
	t.Run("F2.1-F2.3 window", func(t *testing.T) {
		admin.mustPost("/admin/settings", "/admin/settings", settingsForm("21:35", "06:00", 5000), http.StatusSeeOther)
		a.now = utc(2026, 10, 12, 21, 30)
		body := phone.mustGet("/patrol/tonight")
		contains(t, body, "Laporan dibuka pukul 21.35", "Tanggal: Senin, 12 Oktober 2026", "Anton (A-1)", "C-1", "C-10")
		notContains(t, body, "Budi", `type="checkbox"`)

		a.now = utc(2026, 10, 12, 21, 35)
		contains(t, phone.mustGet("/patrol/tonight"), `type="checkbox"`, "Tanggal: Senin, 12 Oktober 2026")

		a.now = utc(2026, 10, 13, 0, 30) // after midnight: still Monday's night
		contains(t, phone.mustGet("/patrol/tonight"), "Tanggal: Senin, 12 Oktober 2026", `type="checkbox"`)

		admin.mustPost("/admin/settings", "/admin/settings", settingsForm("22:00", "06:00", 5000), http.StatusSeeOther)
	})

	// ---- F2.5-F2.8: take 3 photos, check two members, write the situation, then submit.
	// Product-owner change to F2.5/F2.6/F2.11: nothing but photos is saved before
	// "Kirim Laporan", and the report is read-only once sent.
	t.Run("F2.5-F2.8 submit and audit", func(t *testing.T) {
		a.now = utc(2026, 10, 12, 22, 40)
		for i := 0; i < 3; i++ {
			if resp, _ := phone.uploadPhoto(fakeJPEG(100+i), nil); resp.StatusCode != 200 {
				t.Fatalf("photo %d: %d", i, resp.StatusCode)
			}
		}
		contains(t, phone.mustGet("/patrol/tonight"), "3 dari minimal 3 foto", "Kirim Laporan")
		resp, _ := phone.hxPost("/patrol/tonight/submit", url.Values{
			"note": {"Aman, lampu pos mati"}, "present": {"2", "4"}, // Anton, Citra (swap-in)
		})
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("submit: %d", resp.StatusCode)
		}
		body := phone.mustGet("/patrol/tonight") // read-only from now on
		contains(t, body, "Laporan sudah dikirim pukul", "Aman, lampu pos mati")
		notContains(t, body, `type="checkbox"`, "<textarea")
		if n := strings.Count(body, ">Hadir<"); n != 2 {
			t.Errorf("present: %d", n)
		}
		var n int
		a.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE actor = 'ANONYMOUS' AND ip IS NOT NULL AND user_agent IS NOT NULL`).Scan(&n)
		if n != 4 {
			t.Errorf("audit entries: %d, want 4 (3 photos, 1 submit)", n)
		}
	})

	// ---- F3.1: ≤ 5 minutes after the window closes the night is closed and fined.
	t.Run("F3.1 automatic closing", func(t *testing.T) {
		tick(utc(2026, 10, 13, 5, 59))
		contains(t, admin.mustGet("/admin/nights/2026-10-12"), "Status", "–") // not closed yet
		tick(utc(2026, 10, 13, 6, 4))
		body := admin.mustGet("/admin/nights/2026-10-12")
		contains(t, body, "Dilaporkan", "Rp 5.000", `aria-label="C-10: Tidak hadir"`, `aria-label="Anton (A-1): Hadir"`)
	})

	// ---- F3.2 and F3.4: nobody reports the next Monday; the tariff changed meanwhile.
	t.Run("F3.2 and F3.4 no report and frozen tariff", func(t *testing.T) {
		admin.mustPost("/admin/settings", "/admin/settings", settingsForm("22:00", "06:00", 7500), http.StatusSeeOther)
		tick(utc(2026, 10, 20, 6, 4))
		body := admin.mustGet("/admin/nights/2026-10-19")
		contains(t, body, "Tanpa laporan", "Rp 22.500") // 3 members x 7,500
		contains(t, admin.mustGet("/admin/nights/2026-10-12"), "Rp 5.000")
		notContains(t, admin.mustGet("/admin/nights/2026-10-12"), "Rp 7.500")
	})

	// ---- F3.3: the reporter asks for rain, the admin confirms; fines go away and the recap shows "h".
	t.Run("F3.3 rain", func(t *testing.T) {
		a.now = utc(2026, 10, 26, 22, 30)
		phone.hxPost("/patrol/tonight/rain", url.Values{})
		tick(utc(2026, 10, 27, 6, 4))
		contains(t, admin.mustGet("/admin/nights?month=2026-10"), "Menunggu konfirmasi hujan")
		admin.mustPost("/admin/nights", "/admin/nights/2026-10-26/rain/confirm", url.Values{}, http.StatusSeeOther)
		contains(t, admin.mustGet("/admin/nights/2026-10-26"), "Hujan dikonfirmasi.")
	})

	// ---- F4.1-F4.5: the public recap.
	t.Run("F4 public recap", func(t *testing.T) {
		public := a.anonymous()
		body := public.mustGet("/recap/2026/10")
		contains(t, body, "<title>Rekap Ronda Oktober 2026</title>",
			"1/3", "1 hujan · 1 tanpa laporan", // reported nights
			"33%", "2 dari 6 jadwal", // attendance
			"Rp 27.500", "3 peserta kena denda", // 5,000 + 22,500
			`aria-label="12 Oktober: hadir"`, `aria-label="12 Oktober: tidak hadir"`, `aria-label="26 Oktober: hujan"`,
			"3 malam jaga", "Subtotal tim", "Hadir", "Tidak hadir", "Hujan")
		notContains(t, body, "<img", "/photos/")

		sit := public.mustGet("/recap/2026/10/situations")
		contains(t, sit, "Aman, lampu pos mati", "2/3 hadir", "Tanpa laporan", "Hujan", "Tidak ada laporan sampai pukul 06.00.", "Hujan, ronda ditiadakan.")
		notContains(t, sit, "<img", "/photos/")
		if resp, _ := public.do("GET", "/recap/2026/11", nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("future month: %d", resp.StatusCode)
		}
	})

	// ---- F5.1-F5.4: settings are stored, a correction needs a reason, and the QR page works.
	t.Run("F5 admin", func(t *testing.T) {
		contains(t, admin.mustGet("/admin/settings"), `value="7500"`)
		body := admin.mustPost("/admin/nights/2026-10-12", "/admin/nights/2026-10-12/attendance/1",
			url.Values{"status": {"PRESENT"}, "reason": {""}}, http.StatusUnprocessableEntity)
		contains(t, body, "Alasan wajib diisi")
		admin.mustPost("/admin/nights/2026-10-12", "/admin/nights/2026-10-12/attendance/1",
			url.Values{"status": {"PRESENT"}, "reason": {"Datang terlambat, dicatat satpam"}}, http.StatusSeeOther)
		contains(t, admin.mustGet("/admin/nights/2026-10-12"), "Alasan: Datang terlambat, dicatat satpam", "Koreksi absen")
		contains(t, a.anonymous().mustGet("/recap/2026/10"), "Rp 22.500") // 27,500 less C-10's 5,000

		contains(t, admin.mustGet("/admin/qr"), "https://ronda.example.com/patrol/tonight", "Laporan Ronda RT 02")
		if resp, png := admin.do("GET", "/admin/qr.png", nil); resp.StatusCode != 200 || !strings.HasPrefix(png, "\x89PNG") {
			t.Errorf("qr.png: %d", resp.StatusCode)
		}
	})
}
