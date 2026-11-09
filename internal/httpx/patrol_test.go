package httpx

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var hxToken = regexp.MustCompile(`X-CSRF-Token&#34;:&#34;([^&]+)&#34;`)

// hxPost mimics htmx: the CSRF token travels in the X-CSRF-Token header.
func (b *browser) hxPost(path string, form url.Values) (*http.Response, string) {
	b.t.Helper()
	_, page := b.do("GET", "/patrol/tonight", nil)
	m := hxToken.FindStringSubmatch(page)
	if m == nil {
		b.t.Fatalf("no htmx token on the report page:\n%s", page)
	}
	return b.doRaw("POST", path, strings.NewReader(form.Encode()), map[string]string{
		"X-CSRF-Token": m[1], "HX-Request": "true", "Content-Type": "application/x-www-form-urlencoded",
	})
}

func (b *browser) uploadPhoto(data []byte, fields map[string]string) (*http.Response, string) {
	b.t.Helper()
	_, page := b.do("GET", "/patrol/tonight", nil)
	m := hxToken.FindStringSubmatch(page)
	if m == nil {
		b.t.Fatal("no htmx token")
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("photo", "photo.jpg")
	fw.Write(data)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	mw.Close()
	return b.doRaw("POST", "/patrol/tonight/photos", &buf, map[string]string{
		"X-CSRF-Token": m[1], "HX-Request": "true", "Content-Type": mw.FormDataContentType(),
	})
}

// photos uploads n small photos.
func (b *browser) photos(n int) {
	b.t.Helper()
	for i := 0; i < n; i++ {
		if resp, body := b.uploadPhoto(fakeJPEG(20), nil); resp.StatusCode != 200 {
			b.t.Fatalf("photo %d: %d %s", i, resp.StatusCode, body)
		}
	}
}

// submitReport uploads the minimum photos and sends the report form.
func (b *browser) submitReport(form url.Values) {
	b.t.Helper()
	b.photos(3)
	resp, body := b.hxPost("/patrol/tonight/submit", form)
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("HX-Redirect") != "/patrol/tonight" {
		b.t.Fatalf("submit: %d %q %s", resp.StatusCode, resp.Header.Get("HX-Redirect"), body)
	}
}

func fakeJPEG(n int) []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, bytes.Repeat([]byte{7}, n)...)
}

// patrolApp has three members on the Wednesday team and the clock at Wed 22:30 WIB.
func patrolApp(t *testing.T) (*app, *browser) {
	t.Helper()
	a := newApp(t, wibTime(2026, 10, 7, 22, 30))
	admin := a.loggedIn()
	admin.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {"A-1"}, "name": {"Anton"}}, http.StatusSeeOther)
	admin.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {"B-2"}, "name": {"Budi"}}, http.StatusSeeOther)
	admin.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {"c-3"}}, http.StatusSeeOther)
	admin.mustPost("/admin/members", "/admin/members", url.Values{"house_block": {"D-4"}, "name": {"Dewi"}}, http.StatusSeeOther)
	for _, id := range []string{"1", "2", "3"} {
		admin.mustPost("/admin/schedule", "/admin/schedule/rules",
			url.Values{"member_id": {id}, "day_of_week": {"3"}, "effective_from": {"2026-01-01"}}, http.StatusSeeOther)
	}
	return a, a.anonymous()
}

func TestTonightPageClosedAndOpen(t *testing.T) {
	a, b := patrolApp(t)

	a.now = wibTime(2026, 10, 7, 21, 0)
	body := b.mustGet("/patrol/tonight")
	contains(t, body, "Laporan Ronda RT 02", "Tanggal: Rabu, 7 Oktober 2026", "Laporan dibuka pukul 22.00", "Anton (A-1)", "C-3",
		`id="lock-modal"`, `aria-modal="true"`, `data-countdown="3600"`, ">01:00:00<", "lock.js", "inert")
	notContains(t, body, `type="checkbox"`, "photo.js")
	if resp, _ := b.hxPost("/patrol/tonight/submit", url.Values{"note": {"x"}}); resp.StatusCode != http.StatusConflict ||
		resp.Header.Get("HX-Redirect") != "/patrol/tonight" {
		t.Errorf("write outside window: %d redirect=%q", resp.StatusCode, resp.Header.Get("HX-Redirect"))
	}

	a.now = wibTime(2026, 10, 8, 0, 30) // after midnight: still Wednesday's night
	body = b.mustGet("/patrol/tonight")
	contains(t, body, "Tanggal: Rabu, 7 Oktober 2026", `type="checkbox"`, `name="present"`, `hx-post="/patrol/tonight/submit"`, "photo.js", "patrol.js",
		`capture="environment"`, `accept="image/*"`, "Laporkan Hujan", "0 dari minimal 3 foto", `data-count="0"`, `maxlength="1000"`, `name="note"`,
		"Tambah anggota pengganti", "Dewi (D-4)", `data-reload-in="19800"`, "Kirim Laporan", `form="report-form"`, "tidak bisa diubah lagi")
	notContains(t, body, "lock-modal", "lock.js", "/patrol/tonight/attendance", "/patrol/tonight/situation")
	if n := strings.Count(body, `type="checkbox"`); n != 3 {
		t.Errorf("checkboxes: %d", n)
	}
	if got := b.mustGet("/patrol/tonight"); got == "" {
		t.Fatal("empty page")
	}
}

func TestSubmitReport(t *testing.T) {
	_, b := patrolApp(t)

	// The old autosave endpoints are gone.
	for _, path := range []string{"/patrol/tonight/attendance/1", "/patrol/tonight/situation", "/patrol/tonight/walk-in"} {
		if resp, _ := b.hxPost(path, url.Values{"present": {"true"}, "note": {"x"}, "member_id": {"4"}}); resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
	}

	b.photos(2)
	for _, tc := range []struct {
		name   string
		form   url.Values
		status int
		msg    string
	}{
		{"too few photos", url.Values{"note": {"Aman"}}, http.StatusUnprocessableEntity, "Foto belum cukup"},
	} {
		if resp, body := b.hxPost("/patrol/tonight/submit", tc.form); resp.StatusCode != tc.status || !strings.Contains(body, tc.msg) {
			t.Errorf("%s: %d %s", tc.name, resp.StatusCode, body)
		}
	}
	b.photos(1)
	for _, tc := range []struct {
		name   string
		form   url.Values
		status int
		msg    string
	}{
		{"empty note", url.Values{"note": {"  "}}, http.StatusUnprocessableEntity, "Isi kolom situasi"},
		{"note too long", url.Values{"note": {strings.Repeat("x", 1001)}}, http.StatusUnprocessableEntity, "1.000"},
		{"not on the team", url.Values{"note": {"Aman"}, "present": {"4"}}, http.StatusBadRequest, "tidak ada di tim"},
		{"bad member id", url.Values{"note": {"Aman"}, "present": {"abc"}}, http.StatusBadRequest, ""},
		{"walk-in already on team", url.Values{"note": {"Aman"}, "walk_in": {"1"}}, http.StatusConflict, "sudah ada"},
	} {
		if resp, body := b.hxPost("/patrol/tonight/submit", tc.form); resp.StatusCode != tc.status || !strings.Contains(body, tc.msg) {
			t.Errorf("%s: %d %s", tc.name, resp.StatusCode, body)
		}
	}
	// Nothing was saved by the rejected attempts.
	body := b.mustGet("/patrol/tonight")
	contains(t, body, `id="report-form"`)

	resp, frag := b.hxPost("/patrol/tonight/submit", url.Values{"note": {"Aman terkendali"}, "present": {"1"}, "walk_in": {"4"}})
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("HX-Redirect") != "/patrol/tonight" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("submit: %d %q\n%s", resp.StatusCode, resp.Header.Get("HX-Redirect"), frag)
	}

	// The page is now a read-only summary.
	body = b.mustGet("/patrol/tonight")
	contains(t, body, "Laporan sudah dikirim pukul 22.30.", "tidak bisa diubah lagi", "Aman terkendali", "Dewi (D-4)", "pengganti",
		"Hadir", "Tidak hadir", "3 dari minimal 3 foto", "/patrol/tonight/photos/1/thumb", `data-reload-in="27000"`)
	notContains(t, body, `id="report-form"`, `type="checkbox"`, "Kirim Laporan", "Laporkan Hujan", "photo.js", "lock-modal", "<textarea")
	if n := strings.Count(body, ">Hadir<"); n != 2 {
		t.Errorf("present badges: %d", n)
	}

	// Any further write bounces back to the summary.
	for _, path := range []string{"/patrol/tonight/submit", "/patrol/tonight/rain"} {
		if resp, _ := b.hxPost(path, url.Values{"note": {"lagi"}}); resp.StatusCode != http.StatusConflict || resp.Header.Get("HX-Redirect") != "/patrol/tonight" {
			t.Errorf("%s after submit: %d", path, resp.StatusCode)
		}
	}
	if resp, _ := b.uploadPhoto(fakeJPEG(20), nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("photo after submit: %d", resp.StatusCode)
	}
}

func TestSubmitWithoutHtmxRedirects(t *testing.T) {
	_, b := patrolApp(t)
	b.photos(3)
	_, page := b.do("GET", "/patrol/tonight", nil)
	token := hxToken.FindStringSubmatch(page)[1]
	resp, _ := b.do("POST", "/patrol/tonight/submit", url.Values{"note": {"Aman"}, "csrf_token": {token}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/patrol/tonight" {
		t.Fatalf("plain form submit: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestPostWithoutTokenIsRejected(t *testing.T) {
	_, b := patrolApp(t)
	b.mustGet("/patrol/tonight")
	if resp, _ := b.doRaw("POST", "/patrol/tonight/submit", strings.NewReader("note=x"), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestPhotoUpload(t *testing.T) {
	a, b := patrolApp(t)

	resp, frag := b.uploadPhoto(fakeJPEG(200), map[string]string{"lat": "-6.2", "lng": "106.8"})
	if resp.StatusCode != 200 || !strings.Contains(frag, `id="photo-list"`) || !strings.Contains(frag, "1 dari minimal 3 foto") ||
		!strings.Contains(frag, "/patrol/tonight/photos/1/thumb") || !strings.Contains(frag, "Foto ronda pukul 22.30") {
		t.Fatalf("upload: %d\n%s", resp.StatusCode, frag)
	}

	// The thumbnail is served while the night is open ...
	resp, data := b.do("GET", "/patrol/tonight/photos/1/thumb", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" || resp.Header.Get("Cache-Control") != "no-store" || data != string(fakeJPEG(200)) {
		t.Fatalf("thumb: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	// ... and not afterwards.
	a.now = wibTime(2026, 10, 8, 6, 5)
	if resp, _ := b.do("GET", "/patrol/tonight/photos/1/thumb", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("thumb after close: %d", resp.StatusCode)
	}
	a.now = wibTime(2026, 10, 7, 23, 0)

	if resp, _ := b.uploadPhoto([]byte("<html>hi</html>"), nil); resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("non-image: %d", resp.StatusCode)
	}
	if resp, _ := b.uploadPhoto(fakeJPEG(1<<20), nil); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("1 MB + header: %d", resp.StatusCode)
	}
	if resp, _ := b.uploadPhoto(fakeJPEG(3<<20), nil); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("3 MB: %d", resp.StatusCode)
	}
	for i := 2; i <= 10; i++ {
		if resp, _ := b.uploadPhoto(fakeJPEG(20), nil); resp.StatusCode != 200 {
			t.Fatalf("photo %d: %d", i, resp.StatusCode)
		}
	}
	if resp, body := b.uploadPhoto(fakeJPEG(20), nil); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "batas") {
		t.Errorf("11th photo: %d %s", resp.StatusCode, body)
	}
	contains(t, b.mustGet("/patrol/tonight"), "10 dari minimal 3 foto")
}

func TestRainRequestLocksTheReport(t *testing.T) {
	_, b := patrolApp(t)
	resp, _ := b.hxPost("/patrol/tonight/rain", url.Values{})
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("HX-Redirect") != "/patrol/tonight" {
		t.Fatalf("rain: %d %q", resp.StatusCode, resp.Header.Get("HX-Redirect"))
	}
	body := b.mustGet("/patrol/tonight")
	contains(t, body, "Hujan diajukan, menunggu konfirmasi pengurus.", "tidak bisa diubah lagi", "Tidak ada catatan.")
	notContains(t, body, "Laporkan Hujan", "Kirim Laporan", `id="report-form"`, "lock-modal")
}

func TestLockedNightShowsModal(t *testing.T) {
	a, b := patrolApp(t)
	b.hxPost("/patrol/tonight/rain", url.Values{})
	a.loggedIn().mustPost("/admin/nights", "/admin/nights/2026-10-07/rain/confirm", url.Values{}, http.StatusSeeOther)

	// 22:30 on a rained-out night: the next window opens Thursday 22:00, 23.5 hours away.
	body := b.mustGet("/patrol/tonight")
	contains(t, body, `id="lock-modal"`, "Laporan malam ini sudah ditutup.", "Laporan berikutnya dibuka pukul 22.00",
		`data-countdown="84600"`, ">23:30:00<", "Anton (A-1)")
	notContains(t, body, "photo.js", "Laporkan Hujan", "Kirim Laporan")
}

func TestPatrolRateLimit(t *testing.T) {
	_, b := patrolApp(t)
	var last int
	for i := 0; i < 61; i++ {
		resp, _ := b.do("GET", "/patrol/tonight", nil)
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("61st request: %d", last)
	}
}

func TestAuditTrailAfterPublicActions(t *testing.T) {
	a, b := patrolApp(t)
	b.submitReport(url.Values{"note": {"Aman"}, "present": {"1"}})

	rows, err := a.pool.Query(t.Context(), `SELECT action, actor, ip, user_agent FROM audit_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var action, actor string
		var ip, ua *string
		rows.Scan(&action, &actor, &ip, &ua)
		if actor != "ANONYMOUS" || ip == nil || *ip == "" || ua == nil || *ua == "" {
			t.Errorf("%s: actor=%s ip=%v ua=%v", action, actor, ip, ua)
		}
		got = append(got, action)
	}
	if strings.Join(got, ",") != "upload_photo,upload_photo,upload_photo,submit_report" {
		t.Fatalf("audit: %v", got)
	}
}
