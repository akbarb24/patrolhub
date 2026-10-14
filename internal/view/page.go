// Package view holds the templ templates and the small Go helpers they use.
package view

import (
	"context"
	"encoding/json"
	"strings"

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

func nightStatusLabel(s domain.NightStatus) string {
	switch s {
	case domain.NightOpen:
		return "Terbuka"
	case domain.NightReported:
		return "Dilaporkan"
	case domain.NightNoReport:
		return "Tanpa laporan"
	case domain.NightRainRequested:
		return "Menunggu konfirmasi hujan"
	case domain.NightRainedOut:
		return "Hujan"
	}
	return string(s)
}

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
