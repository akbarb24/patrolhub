package httpx

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/akbarbinaji/patrolhub/internal/service"
)

// closedNight runs Wednesday 7 Oct: Anton present, three photos, optionally a rain request,
// then closes it with the closer at 06:05. It returns an admin browser.
func closedNight(t *testing.T, rain bool) (*app, *browser) {
	t.Helper()
	return closedNightWithNote(t, rain, "Aman, lampu pos mati")
}

func closedNightWithNote(t *testing.T, rain bool, note string) (*app, *browser) {
	t.Helper()
	a, reporter := patrolApp(t)
	admin := a.loggedIn()
	admin.mustPost("/admin/settings", "/admin/settings", url.Values{
		"rt_name": {"RT 02"}, "window_open": {"22:00"}, "window_close": {"06:00"}, "fine_per_absence": {"5000"},
		"min_photos": {"3"}, "max_photos": {"10"}, "go_live_date": {"2026-10-01"},
	}, http.StatusSeeOther)

	reporter.hxPost("/patrol/tonight/attendance/1", url.Values{"present": {"true"}})
	reporter.hxPost("/patrol/tonight/situation", url.Values{"note": {note}})
	for i := 0; i < 3; i++ {
		if resp, _ := reporter.uploadPhoto(fakeJPEG(50+i), map[string]string{"lat": "-6.2", "lng": "106.8"}); resp.StatusCode != 200 {
			t.Fatalf("photo %d: %d", i, resp.StatusCode)
		}
	}
	if rain {
		reporter.hxPost("/patrol/tonight/rain", url.Values{})
	}

	a.now = wibTime(2026, 10, 8, 6, 5)
	closing := service.NewClosingService(a.pool, service.NewSettingsService(a.pool))
	if res, err := closing.Tick(context.Background(), a.now); err != nil || res.Closed != 1 {
		t.Fatalf("tick %+v %v", res, err)
	}
	return a, admin
}

func TestNightListAndDetail(t *testing.T) {
	_, admin := closedNight(t, false)

	body := admin.mustGet("/admin/nights?month=2026-10")
	contains(t, body, "Oktober 2026", "Rabu, 7 Oktober 2026", "Dilaporkan", "1/3", ">3<")
	notContains(t, body, "Menunggu konfirmasi hujan", "Perlu ditinjau</span>")

	body = admin.mustGet("/admin/nights/2026-10-07")
	contains(t, body, "Rabu, 7 Oktober 2026", "Anton (A-1)", "Budi (B-2)", "Rp 10.000", "Rp 5.000", "Aman, lampu pos mati",
		"/admin/photos/1", "https://www.google.com/maps?q=-6.200000,106.800000", "Tutup malam", "ANONYMOUS", "SYSTEM", "127.0.0.1", "Tandai hujan", "Koreksi")
	// Markers carry an accessible label.
	contains(t, body, `aria-label="Anton (A-1): Hadir"`, `aria-label="Budi (B-2): Tidak hadir"`)

	if resp, _ := admin.do("GET", "/admin/nights/2026-10-09", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown night: %d", resp.StatusCode)
	}
	if resp, _ := admin.do("GET", "/admin/nights/not-a-date", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("bad date: %d", resp.StatusCode)
	}
}

func TestPhotosNeedAdminLogin(t *testing.T) {
	a, admin := closedNight(t, false)

	resp, data := admin.do("GET", "/admin/photos/1", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" || resp.Header.Get("Cache-Control") != "private, max-age=3600" || data != string(fakeJPEG(50)) {
		t.Fatalf("admin: %d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"))
	}
	if resp, _ := admin.do("GET", "/admin/photos/999", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown photo: %d", resp.StatusCode)
	}

	anon := a.anonymous() // a fresh "incognito window"
	resp, _ = anon.do("GET", "/admin/photos/1", nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/login" {
		t.Fatalf("anonymous: %d -> %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	// The reporter's thumbnail route must not serve a closed night's photo either.
	if resp, _ := anon.do("GET", "/patrol/tonight/photos/1/thumb", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("thumb after close: %d", resp.StatusCode)
	}
}

func TestConfirmRainRemovesFinesAndChangesRecap(t *testing.T) {
	_, admin := closedNight(t, true)

	body := admin.mustGet("/admin/nights?month=2026-10")
	contains(t, body, "Menunggu konfirmasi hujan", "/admin/nights/2026-10-07/rain/confirm", "/admin/nights/2026-10-07/rain/reject")
	notContains(t, body, "Rp 10.000")

	admin.mustPost("/admin/nights", "/admin/nights/2026-10-07/rain/confirm", url.Values{}, http.StatusSeeOther)
	body = admin.mustGet("/admin/nights/2026-10-07")
	contains(t, body, "Hujan dikonfirmasi.", "Hujan", "Konfirmasi hujan")
	notContains(t, body, "Tandai hujan", "Koreksi")
	contains(t, body, "Total denda", "Rp 0")
	contains(t, admin.mustGet("/admin/fines/2026/10"), "Tidak ada denda bulan ini.")
	notContains(t, admin.mustGet("/admin/nights?month=2026-10"), "Menunggu konfirmasi hujan")

	// Confirming again is refused.
	if resp, _ := admin.post("/admin/nights", "/admin/nights/2026-10-07/rain/confirm", url.Values{}); resp.StatusCode != http.StatusConflict {
		t.Errorf("second confirm: %d", resp.StatusCode)
	}
}

func TestRejectRain(t *testing.T) {
	_, admin := closedNight(t, true)
	admin.mustPost("/admin/nights", "/admin/nights/2026-10-07/rain/reject", url.Values{}, http.StatusSeeOther)
	body := admin.mustGet("/admin/nights/2026-10-07")
	contains(t, body, "Pengajuan hujan ditolak.", "Dilaporkan", "Rp 10.000")
	contains(t, admin.mustGet("/admin/fines/2026/10"), "Rp 10.000")
}

func TestMarkRainDirectly(t *testing.T) {
	_, admin := closedNight(t, false)
	admin.mustPost("/admin/nights", "/admin/nights/2026-10-07/rain", url.Values{}, http.StatusSeeOther)
	contains(t, admin.mustGet("/admin/nights/2026-10-07"), "Malam ditandai hujan.", `aria-label="Anton (A-1): Hujan"`)
	contains(t, admin.mustGet("/admin/fines/2026/10"), "Tidak ada denda bulan ini.")
}

func TestCorrectionRequiresReason(t *testing.T) {
	_, admin := closedNight(t, false)

	body := admin.mustPost("/admin/nights/2026-10-07", "/admin/nights/2026-10-07/attendance/2",
		url.Values{"status": {"PRESENT"}, "reason": {""}}, http.StatusUnprocessableEntity)
	contains(t, body, "Alasan wajib diisi (minimal 5 karakter).")
	notContains(t, body, "Koreksi tersimpan.")
	contains(t, admin.mustGet("/admin/nights/2026-10-07"), `aria-label="Budi (B-2): Tidak hadir"`)

	admin.mustPost("/admin/nights/2026-10-07", "/admin/nights/2026-10-07/attendance/2",
		url.Values{"status": {"PRESENT"}, "reason": {"Datang terlambat, dicatat satpam"}}, http.StatusSeeOther)
	body = admin.mustGet("/admin/nights/2026-10-07")
	contains(t, body, "Koreksi tersimpan.", `aria-label="Budi (B-2): Hadir"`, "Koreksi absen", "Alasan: Datang terlambat, dicatat satpam", "admin:1")
	contains(t, admin.mustGet("/admin/fines/2026/10"), "Rp 5.000") // only C-3 is still fined

	if resp, _ := admin.post("/admin/nights/2026-10-07", "/admin/nights/2026-10-07/attendance/99", url.Values{"status": {"PRESENT"}, "reason": {"tidak ada orangnya"}}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown member: %d", resp.StatusCode)
	}
}

func TestFinesAndPaymentsPage(t *testing.T) {
	a, admin := closedNight(t, false)

	resp, _ := admin.do("GET", "/admin/fines", nil)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/admin/fines/2026/10" {
		t.Fatalf("redirect: %d -> %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	body := admin.mustGet("/admin/fines/2026/10")
	contains(t, body, "Oktober 2026", "Budi (B-2)", "C-3", "Rp 10.000", `value="5000"`, "/admin/fines/2026/09", "/admin/fines/2026/11")
	notContains(t, body, "Anton")

	admin.mustPost("/admin/fines/2026/10", "/admin/fines/2026/10/payments",
		url.Values{"member_id": {"2"}, "amount": {"5000"}, "paid_at": {"2026-11-03"}}, http.StatusSeeOther)
	body = admin.mustGet("/admin/fines/2026/10")
	contains(t, body, "Pembayaran dicatat.", "Total dibayar", "Rp 5.000", "Selasa, 3 November 2026")

	body = admin.mustPost("/admin/fines/2026/10", "/admin/fines/2026/10/payments",
		url.Values{"member_id": {"3"}, "amount": {"0"}, "paid_at": {"2026-11-03"}}, http.StatusUnprocessableEntity)
	contains(t, body, "Nominal harus lebih dari 0.")
	body = admin.mustPost("/admin/fines/2026/10", "/admin/fines/2026/10/payments",
		url.Values{"member_id": {"3"}, "amount": {"5000"}, "paid_at": {"kemarin"}}, http.StatusUnprocessableEntity)
	contains(t, body, "Tanggal harus berformat")

	admin.mustPost("/admin/fines/2026/10", "/admin/fines/2026/10/payments/1/delete", url.Values{}, http.StatusSeeOther)
	notContains(t, admin.mustGet("/admin/fines/2026/10"), "Selasa, 3 November 2026")
	if resp, _ := admin.post("/admin/fines/2026/10", "/admin/fines/2026/10/payments/77/delete", url.Values{}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown payment: %d", resp.StatusCode)
	}
	if resp, _ := admin.do("GET", "/admin/fines/2026/13", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("bad month: %d", resp.StatusCode)
	}

	// Everything above is locked behind the login.
	anon := a.anonymous()
	for _, p := range []string{"/admin/nights", "/admin/nights/2026-10-07", "/admin/fines", "/admin/fines/2026/10"} {
		if resp, _ := anon.do("GET", p, nil); resp.StatusCode != http.StatusSeeOther {
			t.Errorf("%s anonymous: %d", p, resp.StatusCode)
		}
	}
}
