package httpx

import (
	"encoding/csv"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestCopyRecapText(t *testing.T) {
	a, _ := closedNight(t, false)
	b := a.anonymous()

	_, page := b.do("GET", "/recap/2026/10", nil)
	contains(t, page, "Salin rekap", `data-copy="month"`, `data-copy="week"`, `data-base="/recap/2026/10/text"`, "/static/js/recap.js", "Rekap disalin")

	resp, body := b.do("GET", "/recap/2026/10/text?range=month", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("%d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	want := strings.Join([]string{
		"*Rekap Ronda RT 02 — Oktober 2026*",
		"Malam terlapor: 1/1 (hujan 0, tanpa laporan 0)",
		"Kehadiran: 33%",
		"Total denda: Rp 10.000",
		"",
		"Denda per peserta:",
		"- Budi (B-2): Rp 5.000",
		"- C-3: Rp 5.000",
		"",
		"Detail: https://ronda.example.com/recap/2026/10",
		"",
	}, "\n")
	if body != want {
		t.Fatalf("got:\n%s\nwant:\n%s", body, want)
	}

	_, week := b.do("GET", "/recap/2026/10/text?range=week", nil)
	contains(t, week, "7 malam terakhir (1–7 Oktober 2026)", "Rp 10.000")

	if resp, _ := b.do("GET", "/recap/2026/10/text?range=year", nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad range: %d", resp.StatusCode)
	}
	if resp, _ := b.do("GET", "/recap/2026/10/text", nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing range: %d", resp.StatusCode)
	}
	if resp, _ := b.do("GET", "/recap/2026/11/text?range=month", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("future month: %d", resp.StatusCode)
	}
}

func TestCSVExport(t *testing.T) {
	a, admin := closedNight(t, false)

	resp, body := admin.do("GET", "/admin/export/2026/10.csv", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/csv; charset=utf-8" ||
		resp.Header.Get("Content-Disposition") != `attachment; filename="rekap-ronda-2026-10.csv"` {
		t.Fatalf("%d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Disposition"))
	}
	if !strings.HasPrefix(body, "\xEF\xBB\xBF") {
		t.Fatal("missing UTF-8 byte order mark")
	}
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"tanggal", "hari", "status_malam", "blok", "nama", "status_absen", "denda"},
		{"2026-10-07", "Rabu", "Dilaporkan", "A-1", "Anton", "Hadir", "0"},
		{"2026-10-07", "Rabu", "Dilaporkan", "B-2", "Budi", "Tidak hadir", "5000"},
		{"2026-10-07", "Rabu", "Dilaporkan", "C-3", "", "Tidak hadir", "5000"},
	}
	if len(recs) != len(want) {
		t.Fatalf("rows: %v", recs)
	}
	for i := range want {
		if strings.Join(recs[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("row %d: %v, want %v", i, recs[i], want[i])
		}
	}

	// An empty month still downloads, with only the header.
	_, body = admin.do("GET", "/admin/export/2026/09.csv", nil)
	if strings.Count(body, "\n") != 1 {
		t.Errorf("empty month: %q", body)
	}
	if resp, _ := admin.do("GET", "/admin/export/2026/11.csv", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("future month: %d", resp.StatusCode)
	}
	if resp, _ := admin.do("GET", "/admin/export/2026/13.csv", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("bad month: %d", resp.StatusCode)
	}

	// The export links appear in the admin UI, and anonymous visitors cannot download.
	contains(t, admin.mustGet("/admin/fines/2026/10"), "/admin/export/2026/10.csv")
	contains(t, admin.mustGet("/admin/nights?month=2026-10"), "/admin/export/2026/10.csv")
	if resp, _ := a.anonymous().do("GET", "/admin/export/2026/10.csv", nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous export: %d", resp.StatusCode)
	}
}

func TestCSVNeutralisesSpreadsheetFormulas(t *testing.T) {
	for in, want := range map[string]string{
		"=1+1": "'=1+1", "+62812": "'+62812", "-5": "'-5", "@SUM(A1)": "'@SUM(A1)", "\tx": "'\tx",
		"Anton": "Anton", "C-10": "C-10", "": "",
	} {
		if got := csvSafe(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}

	a, admin := closedNight(t, false)
	admin.mustPost("/admin/members/1/edit", "/admin/members/1", url.Values{"house_block": {"A-1"}, "name": {"=HYPERLINK(\"http://evil\")"}}, http.StatusSeeOther)
	_, body := admin.do("GET", "/admin/export/2026/10.csv", nil)
	if strings.Contains(body, ",=HYPERLINK") || !strings.Contains(body, "'=HYPERLINK") {
		t.Fatalf("formula not neutralised:\n%s", body)
	}
	_ = a
}

func TestXLSXExport(t *testing.T) {
	a, admin := closedNight(t, false)

	resp, body := admin.do("GET", "/admin/export/2026/10.xlsx", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" ||
		resp.Header.Get("Content-Disposition") != `attachment; filename="rekap-ronda-2026-10.xlsx"` {
		t.Fatalf("%d %q %q", resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Disposition"))
	}
	f, err := excelize.OpenReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	const sheet = "Oktober 2026"
	// 7 October 2026 is a Wednesday: column I (day 7); the fine column follows day 31 (AH).
	for cell, want := range map[string]string{
		"A1": "Bulan Oktober", "B2": "Tanggal", "I1": "R", "I2": "7", "AH1": "Denda Ronda Bulan Oktober",
		"A3": "Rabu", "B3": "Anton (A-1)", "I3": "v", "AH3": "Rp 0",
		"B4": "Budi (B-2)", "I4": "1", "AH4": "Rp 5,000",
		"B5": "C-3", "I5": "1", "H5": "",
		"A6": "TOTAL DENDA RONDA", "AH6": "Rp 10,000",
	} {
		if got, _ := f.GetCellValue(sheet, cell); got != want {
			t.Errorf("%s = %q, want %q", cell, got, want)
		}
	}
	merges, _ := f.GetMergeCells(sheet)
	var refs []string
	for _, m := range merges {
		refs = append(refs, m.GetStartAxis()+":"+m.GetEndAxis())
	}
	contains(t, strings.Join(refs, " "), "A1:A2", "AH1:AH2", "A3:A5", "A6:AG6")

	// A confirmed rain night shows h.
	_, rainAdmin := closedNight(t, true)
	rainAdmin.mustPost("/admin/nights", "/admin/nights/2026-10-07/rain/confirm", url.Values{}, http.StatusSeeOther)
	_, body = rainAdmin.do("GET", "/admin/export/2026/10.xlsx", nil)
	f, err = excelize.OpenReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := f.GetCellValue(sheet, "I4"); got != "h" {
		t.Errorf("rain cell = %q", got)
	}

	if resp, _ := admin.do("GET", "/admin/export/2026/11.xlsx", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("future month: %d", resp.StatusCode)
	}
	contains(t, admin.mustGet("/admin/fines/2026/10"), "/admin/export/2026/10.xlsx")
	contains(t, admin.mustGet("/admin/nights?month=2026-10"), "/admin/export/2026/10.xlsx")
	if resp, _ := a.anonymous().do("GET", "/admin/export/2026/10.xlsx", nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous export: %d", resp.StatusCode)
	}
}
