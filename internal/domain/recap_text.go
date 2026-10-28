package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// MonthText is the compact recap of a whole month for pasting into a WhatsApp group.
// nights must hold only closed nights; those outside the month are ignored.
func MonthText(rtName string, year int, month time.Month, nights []NightRow, detailURL string) string {
	title := fmt.Sprintf("%s %d", MonthNameID(month), year)
	return recapText(rtName, title, inMonth(year, month, nights), detailURL)
}

// WeekText covers the last seven nights of the month: the seven calendar days that end
// with the latest closed night, clipped to the month. Nothing outside the month is used.
func WeekText(rtName string, year int, month time.Month, nights []NightRow, detailURL string) string {
	all := inMonth(year, month, nights)
	if len(all) == 0 {
		return recapText(rtName, "7 malam terakhir", nil, detailURL)
	}
	latest := all[0].Date
	for _, n := range all {
		if n.Date.Compare(latest) > 0 {
			latest = n.Date
		}
	}
	start := latest.AddDays(-6)
	if start.Month != month {
		start = Date{Year: year, Month: month, Day: 1}
	}
	var picked []NightRow
	for _, n := range all {
		if n.Date.Compare(start) >= 0 {
			picked = append(picked, n)
		}
	}
	span := fmt.Sprintf("%d–%d", start.Day, latest.Day)
	if start == latest {
		span = fmt.Sprint(latest.Day)
	}
	title := fmt.Sprintf("7 malam terakhir (%s %s %d)", span, MonthNameID(month), year)
	return recapText(rtName, title, picked, detailURL)
}

func inMonth(year int, month time.Month, nights []NightRow) []NightRow {
	var out []NightRow
	for _, n := range nights {
		if n.Date.Year == year && n.Date.Month == month {
			out = append(out, n)
		}
	}
	return out
}

func recapText(rtName, title string, nights []NightRow, detailURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "*Rekap Ronda %s — %s*\n", rtName, title)

	if len(nights) == 0 {
		b.WriteString("Belum ada data untuk periode ini.\n")
	} else {
		s := buildSummary(nights)
		fmt.Fprintf(&b, "Malam terlapor: %d/%d (hujan %d, tanpa laporan %d)\n", s.Reported, s.NightsTotal, s.Rain, s.NoReport)
		fmt.Fprintf(&b, "Kehadiran: %d%%\n", s.RatePercent)
		fmt.Fprintf(&b, "Total denda: %s\n", FormatRupiah(s.TotalFine))

		if fines := finesByMember(nights); len(fines) > 0 {
			b.WriteString("\nDenda per peserta:\n")
			for _, f := range fines {
				fmt.Fprintf(&b, "- %s: %s\n", f.name, FormatRupiah(f.total))
			}
		}
	}
	if detailURL != "" {
		fmt.Fprintf(&b, "\nDetail: %s\n", detailURL)
	}
	return b.String()
}

type memberFine struct {
	name  string
	total int
}

// finesByMember sums fines per member, drops members without a fine and sorts by name (A–Z).
func finesByMember(nights []NightRow) []memberFine {
	totals := map[int64]*memberFine{}
	for _, n := range nights {
		for _, a := range n.Attendance {
			f, ok := totals[a.MemberID]
			if !ok {
				f = &memberFine{name: a.DisplayName}
				totals[a.MemberID] = f
			}
			f.total += a.Fine
		}
	}
	var out []memberFine
	for _, f := range totals {
		if f.total > 0 {
			out = append(out, *f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].name), strings.ToLower(out[j].name)
		if a != b {
			return a < b
		}
		return out[i].name < out[j].name
	})
	return out
}

// NightReportText is the report of one closed night sent to the WhatsApp group.
// photosURL links to the night's photos; empty means the night has none.
func NightReportText(rtName string, n NightRow, photosURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ABSEN RONDA %s\n%s\n", strings.ToUpper(strings.TrimSpace(rtName)), FormatShortDateID(n.Date))
	if n.Status != NightReported {
		fmt.Fprintf(&b, "Status: %s\n", NightStatusLabel(n.Status))
	}
	for _, a := range n.Attendance {
		fmt.Fprintf(&b, "- %s: %s\n", a.DisplayName, reportStatus(a.Status))
	}
	note := strings.TrimSpace(n.Note)
	if note == "" {
		note = "-"
	}
	fmt.Fprintf(&b, "Situasi:\n%s\n", note)
	if photosURL == "" {
		b.WriteString("Foto: -\n")
	} else {
		fmt.Fprintf(&b, "Foto:\n%s\n", photosURL)
	}
	return b.String()
}

func reportStatus(s AttendanceStatus) string {
	switch s {
	case Present:
		return "hadir"
	case Absent:
		return "absen"
	case RainedOut:
		return "hujan"
	}
	return strings.ToLower(string(s))
}
