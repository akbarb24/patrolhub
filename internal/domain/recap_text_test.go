package domain

import (
	"strings"
	"testing"
	"time"
)

const detail = "https://ronda.example.com/recap/2026/09"

func TestMonthText(t *testing.T) {
	want := strings.Join([]string{
		"*Rekap Ronda RT 02 — September 2026*",
		"Malam terlapor: 2/4 (hujan 1, tanpa laporan 1)",
		"Kehadiran: 38%",
		"Total denda: Rp 25.000",
		"",
		"Denda per peserta:",
		"- C-10: Rp 10.000",
		"- E-10: Rp 5.000",
		"- Gemilang (D-1): Rp 5.000",
		"- Yusuf (B-3): Rp 5.000",
		"",
		"Detail: " + detail,
		"",
	}, "\n")
	if got := MonthText("RT 02", 2026, time.September, fixtureNights(), detail); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWeekText(t *testing.T) {
	want := strings.Join([]string{
		"*Rekap Ronda RT 02 — 7 malam terakhir (9–15 September 2026)*",
		"Malam terlapor: 1/2 (hujan 0, tanpa laporan 1)",
		"Kehadiran: 20%",
		"Total denda: Rp 20.000",
		"",
		"Denda per peserta:",
		"- C-10: Rp 5.000",
		"- E-10: Rp 5.000",
		"- Gemilang (D-1): Rp 5.000",
		"- Yusuf (B-3): Rp 5.000",
		"",
		"Detail: " + detail,
		"",
	}, "\n")
	if got := WeekText("RT 02", 2026, time.September, fixtureNights(), detail); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestWeekTextClipsToTheMonthAndHandlesOneNight(t *testing.T) {
	nights := []NightRow{{Date{2026, 9, 1}, NightReported, "Aman", []AttendanceRow{{1, "A-1", Present, 0}}}}
	got := WeekText("RT 02", 2026, time.September, nights, "")
	if !strings.Contains(got, "7 malam terakhir (1 September 2026)") {
		t.Fatalf("single day title:\n%s", got)
	}
	// The latest night is the 5th; six days back would be in August, so the window starts on the 1st.
	nights = append(nights, NightRow{Date{2026, 9, 5}, NightReported, "", []AttendanceRow{{1, "A-1", Present, 0}}})
	got = WeekText("RT 02", 2026, time.September, nights, "")
	if !strings.Contains(got, "(1–5 September 2026)") || !strings.Contains(got, "Malam terlapor: 2/2") {
		t.Fatalf("clipped to the first of the month:\n%s", got)
	}
	// A night more than six days before the latest is left out.
	nights = append(nights, NightRow{Date{2026, 9, 20}, NightReported, "", []AttendanceRow{{1, "A-1", Present, 0}}})
	got = WeekText("RT 02", 2026, time.September, nights, "")
	if !strings.Contains(got, "(14–20 September 2026)") || !strings.Contains(got, "Malam terlapor: 1/1") {
		t.Fatalf("only the last seven days:\n%s", got)
	}
}

func TestTextWithoutFinesOrData(t *testing.T) {
	clean := []NightRow{{Date{2026, 9, 7}, NightReported, "Aman", []AttendanceRow{{1, "A-1", Present, 0}}}}
	got := MonthText("RT 02", 2026, time.September, clean, "")
	if strings.Contains(got, "Denda per peserta") || strings.Contains(got, "Detail:") || !strings.Contains(got, "Total denda: Rp 0") {
		t.Fatalf("no fines:\n%s", got)
	}

	empty := MonthText("RT 02", 2026, time.September, nil, detail)
	if !strings.Contains(empty, "Belum ada data untuk periode ini.") || strings.Contains(empty, "Kehadiran") {
		t.Fatalf("no data:\n%s", empty)
	}
	if got := WeekText("RT 02", 2026, time.September, nil, ""); !strings.Contains(got, "Belum ada data") {
		t.Fatalf("no data (week):\n%s", got)
	}
	if got := MonthText("RT 02", 2026, time.August, fixtureNights(), ""); !strings.Contains(got, "Rp 5.000") || strings.Contains(got, "Malam terlapor: 2/4") {
		t.Fatalf("August should only contain the 31st:\n%s", got)
	}
}

func TestLabels(t *testing.T) {
	for s, want := range map[NightStatus]string{NightOpen: "Terbuka", NightReported: "Dilaporkan", NightNoReport: "Tanpa laporan", NightRainRequested: "Menunggu konfirmasi hujan", NightRainedOut: "Hujan", "X": "X"} {
		if NightStatusLabel(s) != want {
			t.Errorf("%s: %s", s, NightStatusLabel(s))
		}
	}
	for s, want := range map[AttendanceStatus]string{Present: "Hadir", Absent: "Tidak hadir", RainedOut: "Hujan", "X": "X"} {
		if AttendanceLabel(s) != want {
			t.Errorf("%s: %s", s, AttendanceLabel(s))
		}
	}
}

func TestNightReportText(t *testing.T) {
	att := []AttendanceRow{{1, "Abc (A-1)", Present, 0}, {2, "Bcde (B-2)", Present, 0}, {3, "C-10", Absent, 5000}}
	rain := []AttendanceRow{{1, "Abc (A-1)", RainedOut, 0}, {3, "C-10", RainedOut, 0}}
	absent := []AttendanceRow{{1, "Abc (A-1)", Absent, 5000}}
	const photos = "https://ronda.example.com/shared/tok"
	date := Date{2026, 10, 28}
	lines := func(l ...string) string { return strings.Join(l, "\n") + "\n" }

	tests := []struct {
		name   string
		rt     string
		night  NightRow
		photos string
		want   string
	}{
		{"reported", "RT 02", NightRow{date, NightReported, "Aman", att}, photos, lines(
			"ABSEN RONDA RT 02", "Rabu, 28 Okt 2026",
			"- Abc (A-1): hadir", "- Bcde (B-2): hadir", "- C-10: absen",
			"Situasi:", "Aman", "Foto:", photos)},
		{"lower-case RT name, empty note, no photos", " rt 02 ", NightRow{date, NightReported, "  ", att[:1]}, "", lines(
			"ABSEN RONDA RT 02", "Rabu, 28 Okt 2026", "- Abc (A-1): hadir", "Situasi:", "-", "Foto: -")},
		{"rained out", "RT 02", NightRow{date, NightRainedOut, "", rain}, "", lines(
			"ABSEN RONDA RT 02", "Rabu, 28 Okt 2026", "Status: Hujan",
			"- Abc (A-1): hujan", "- C-10: hujan", "Situasi:", "-", "Foto: -")},
		{"rain requested", "RT 02", NightRow{date, NightRainRequested, "Hujan deras", att[:1]}, photos, lines(
			"ABSEN RONDA RT 02", "Rabu, 28 Okt 2026", "Status: Menunggu konfirmasi hujan",
			"- Abc (A-1): hadir", "Situasi:", "Hujan deras", "Foto:", photos)},
		{"no report", "RT 02", NightRow{date, NightNoReport, "", absent}, "", lines(
			"ABSEN RONDA RT 02", "Rabu, 28 Okt 2026", "Status: Tanpa laporan",
			"- Abc (A-1): absen", "Situasi:", "-", "Foto: -")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NightReportText(tc.rt, tc.night, tc.photos); got != tc.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}
