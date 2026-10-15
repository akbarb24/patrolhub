package httpx

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestRecapRedirectsToCurrentMonth(t *testing.T) {
	a := newApp(t, wibTime(2026, 10, 14, 12, 0))
	for _, p := range []string{"/", "/recap"} {
		resp, _ := a.anonymous().do("GET", p, nil)
		loc := resp.Header.Get("Location")
		if resp.StatusCode != http.StatusFound || (p == "/recap" && loc != "/recap/2026/10") {
			t.Errorf("%s: %d -> %q", p, resp.StatusCode, loc)
		}
	}
}

func TestRecapSummary(t *testing.T) {
	a, _ := closedNight(t, false)
	b := a.anonymous()

	resp, body := b.do("GET", "/recap/2026/10", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "private, max-age=300" {
		t.Fatalf("%d %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	contains(t, body,
		"<title>Rekap Ronda Oktober 2026</title>", "<h1", "Rekap Ronda", "Oktober 2026",
		"Malam terlapor", "1/1", "0 hujan · 0 tanpa laporan",
		"Kehadiran", "33%", "1 dari 3 jadwal",
		"Total denda Oktober", "Rp 10.000", "2 peserta kena denda · Rp 5.000 / tidak hadir",
		"Hadir", "Tidak hadir", "Hujan", // legend
		"Rabu", "1 malam jaga", "Subtotal tim",
		`aria-label="7 Oktober: hadir"`, `aria-label="7 Oktober: tidak hadir"`,
		`aria-current="page"`, `href="/recap/2026/10/situations"`, `href="/recap/2026/09"`,
	)
	if n := strings.Count(body, "Belum ada malam jaga bulan ini."); n != 6 {
		t.Errorf("empty day cards: %d, want 6", n)
	}
	if n := strings.Count(body, "<section class=\"sticker p-5\" aria-labelledby=\"card-"); n != 7 {
		t.Errorf("day cards: %d, want 7", n)
	}
	// The current month cannot be navigated past.
	contains(t, body, `aria-disabled="true"`)
	notContains(t, body, `href="/recap/2026/11"`)
	contains(t, body, "Salin rekap")

	_, body = b.do("GET", "/recap/2026/09", nil)
	contains(t, body, "Belum ada data untuk bulan ini.", `href="/recap/2026/10"`, `href="/recap/2026/08"`)
	notContains(t, body, `aria-disabled="true"`)
}

func TestRecapRainConfirmationChangesIt(t *testing.T) {
	a, admin := closedNight(t, true)
	b := a.anonymous()

	// A pending request still counts as reported, but nobody is fined until an admin decides.
	_, body := b.do("GET", "/recap/2026/10", nil)
	contains(t, body, "1/1", "Rp 0")
	notContains(t, body, "Rp 10.000")

	admin.mustPost("/admin/nights", "/admin/nights/2026-10-07/rain/confirm", url.Values{}, http.StatusSeeOther)
	_, body = b.do("GET", "/recap/2026/10", nil)
	contains(t, body, "1 hujan", "0%", "Rp 0", "0 peserta kena denda", `aria-label="7 Oktober: hujan"`)
	notContains(t, body, "Rp 10.000")
}

func TestRecapSituations(t *testing.T) {
	a, _ := closedNight(t, false)
	b := a.anonymous()
	_, body := b.do("GET", "/recap/2026/10/situations", nil)
	contains(t, body, "Aman, lampu pos mati", "1/3 hadir", "Catatan khusus", "Semua malam", "Ada catatan khusus", "Rab", ">7<")
	notContains(t, body, "<img", "/photos/")

	_, body = b.do("GET", "/recap/2026/10/situations?special=1", nil)
	contains(t, body, "Aman, lampu pos mati")

	// A plain "Aman" is not special and disappears under the filter.
	a2, _ := closedNightWithNote(t, false, "Aman")
	b2 := a2.anonymous()
	_, body = b2.do("GET", "/recap/2026/10/situations", nil)
	contains(t, body, ">Aman<")
	notContains(t, body, "Catatan khusus</span>")
	_, body = b2.do("GET", "/recap/2026/10/situations?special=1", nil)
	notContains(t, body, ">Aman<")
}

func TestRecapOnlyShowsClosedNights(t *testing.T) {
	a, reporter := patrolApp(t) // Wed 22:30, the night is open
	reporter.hxPost("/patrol/tonight/attendance/1", url.Values{"present": {"true"}})
	_, body := a.anonymous().do("GET", "/recap/2026/10", nil)
	contains(t, body, "Belum ada data untuk bulan ini.")
	notContains(t, body, "Anton")
}

func TestRecapNotFound(t *testing.T) {
	a := newApp(t, wibTime(2026, 10, 14, 12, 0))
	b := a.anonymous()
	for _, p := range []string{"/recap/2026/11", "/recap/2027/01", "/recap/2026/13", "/recap/2026/00", "/recap/2026/1", "/recap/abcd/10", "/recap/2026/10x", "/recap/1999/12"} {
		if resp, _ := b.do("GET", p+"", nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
		if resp, _ := b.do("GET", p+"/situations", nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s/situations: %d", p, resp.StatusCode)
		}
	}
}

func TestRecapNeverExposesPhonesOrPhotos(t *testing.T) {
	a, admin := closedNight(t, false)
	admin.mustPost("/admin/members/1/edit", "/admin/members/1", url.Values{
		"house_block": {"A-1"}, "name": {"Anton"}, "phone": {"081234567890"}, "notes": {"rahasia internal"},
	}, http.StatusSeeOther)
	b := a.anonymous()
	for _, p := range []string{"/recap/2026/10", "/recap/2026/10/situations"} {
		_, body := b.do("GET", p, nil)
		notContains(t, body, "081234567890", "rahasia internal", "<img", "/photos/", "latitude", "maps?q")
	}
}

func gzipped(t *testing.T, b *browser, path string) int {
	t.Helper()
	resp, body := b.doRaw("GET", path, nil, map[string]string{"Accept-Encoding": "gzip"})
	if resp.StatusCode != 200 {
		t.Fatalf("%s: %d", path, resp.StatusCode)
	}
	if resp.Header.Get("Content-Encoding") != "gzip" {
		// Small bodies may be sent uncompressed; measure their gzip size anyway.
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		io.WriteString(zw, body)
		zw.Close()
		return buf.Len()
	}
	return len(body)
}

func TestPageWeightStaysUnder200KB(t *testing.T) {
	a, _ := closedNight(t, false)
	b := a.anonymous()
	assets := regexp.MustCompile(`(?:href|src)="(/static/[^"]+)"`)

	for _, page := range []string{"/recap/2026/10", "/recap/2026/10/situations", "/patrol/tonight"} {
		_, html := b.do("GET", page, nil)
		total := gzipped(t, b, page)
		for _, m := range assets.FindAllStringSubmatch(html, -1) {
			total += gzipped(t, b, m[1])
		}
		t.Logf("%s: %d bytes transferred (gzip)", page, total)
		if total > 200*1024 {
			t.Errorf("%s transfers %d KB, limit is 200 KB", page, total/1024)
		}
	}
}
