// Package view holds the templ templates and the small Go helpers they use.
package view

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/akbarbinaji/patrolhub/internal/config"
	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/service"
)

// PageData is per-request data every layout needs. The HTTP layer puts it in the
// context so page templates do not have to thread it through every call.
type PageData struct {
	CSRFToken string
	RTName    string
}

type pageKey struct{}

func WithPage(ctx context.Context, p PageData) context.Context {
	return context.WithValue(ctx, pageKey{}, p)
}

func Page(ctx context.Context) PageData {
	p, _ := ctx.Value(pageKey{}).(PageData)
	return p
}

// hxHeaders is the value of the hx-headers attribute that makes HTMX send the CSRF token.
func hxHeaders(ctx context.Context) string {
	b, _ := json.Marshal(map[string]string{"X-CSRF-Token": Page(ctx).CSRFToken})
	return string(b)
}

func brand(ctx context.Context) string {
	if rt := Page(ctx).RTName; rt != "" {
		return "PatrolHub · " + rt
	}
	return "PatrolHub"
}

func nightStatusLabel(s domain.NightStatus) string { return domain.NightStatusLabel(s) }

func nightStatusClass(s domain.NightStatus) string {
	switch s {
	case domain.NightReported:
		return "badge-primary"
	case domain.NightNoReport:
		return "badge-error"
	case domain.NightRainRequested:
		return "badge-warning"
	case domain.NightRainedOut:
		return "badge-info"
	}
	return "badge-ghost"
}

func flashClass(kind string) string {
	switch kind {
	case "success":
		return "alert-success"
	case "error":
		return "alert-error"
	case "warning":
		return "alert-warning"
	}
	return "alert-info"
}

func joinTeam(team []service.TeamEntry) string {
	names := make([]string, len(team))
	for i, t := range team {
		names[i] = t.DisplayName
	}
	return strings.Join(names, ", ")
}

// clockText renders a time as 22.41 in Jakarta, matching how people write time in Indonesia.
func ClockText(t time.Time) string { return t.In(config.Jakarta).Format("15.04") }

func clockDotted(c domain.Clock) string { return fmt.Sprintf("%02d.%02d", c.Hour, c.Min) }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// ShortStamp renders "7 Okt 2026 22.41" in Jakarta.
func ShortStamp(t time.Time) string {
	t = t.In(config.Jakarta)
	return fmt.Sprintf("%d %s %d %s", t.Day(), domain.MonthNameID(t.Month())[:3], t.Year(), t.Format("15.04"))
}

func stampPtr(t *time.Time) string {
	if t == nil {
		return "–"
	}
	return ShortStamp(*t)
}

func attendanceLabel(s domain.AttendanceStatus) string { return domain.AttendanceLabel(s) }

func auditLabel(action string) string {
	labels := map[string]string{
		"toggle_attendance": "Ubah kehadiran", "update_situation": "Ubah situasi", "upload_photo": "Unggah foto",
		"request_rain": "Ajukan hujan", "add_walk_in": "Tambah pengganti", "close_night": "Tutup malam",
		"mark_rain": "Tandai hujan", "confirm_rain": "Konfirmasi hujan", "reject_rain": "Tolak hujan",
		"correct_attendance": "Koreksi absen",
	}
	if l, ok := labels[action]; ok {
		return l
	}
	return action
}

func mapsURL(lat, lng *float64) string {
	if lat == nil || lng == nil {
		return ""
	}
	return fmt.Sprintf("https://www.google.com/maps?q=%.6f,%.6f", *lat, *lng)
}

func rupiah(n int) string { return domain.FormatRupiah(n) }

func rupiahNeg(n int) string {
	if n < 0 {
		return "Lebih bayar " + domain.FormatRupiah(-n)
	}
	return domain.FormatRupiah(n)
}

// gridStyle lays out one day-card row: name, one column per duty date, fine.
func gridStyle(dates int) templ.SafeCSS {
	return templ.SafeCSS(fmt.Sprintf("grid-template-columns: minmax(0,1fr) repeat(%d, 1.75rem) 4.25rem", dates))
}

func dotLabel(d domain.Date, s domain.AttendanceStatus) string {
	text := map[domain.AttendanceStatus]string{domain.Present: "hadir", domain.Absent: "tidak hadir", domain.RainedOut: "hujan"}[s]
	return fmt.Sprintf("%d %s: %s", d.Day, domain.MonthNameID(d.Month), text)
}

func fineCell(n int) string {
	if n == 0 {
		return "–"
	}
	return domain.FormatRupiah(n)
}

func weekdayShort(name string) string {
	if len(name) > 3 {
		return name[:3]
	}
	return name
}
