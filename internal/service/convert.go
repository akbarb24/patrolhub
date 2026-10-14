package service

import (
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/akbarbinaji/patrolhub/internal/domain"
)

func pgDate(d domain.Date) pgtype.Date {
	return pgtype.Date{Time: time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC), Valid: true}
}

func pgDatePtr(d *domain.Date) pgtype.Date {
	if d == nil {
		return pgtype.Date{}
	}
	return pgDate(*d)
}

func domainDate(d pgtype.Date) domain.Date {
	return domain.Date{Year: d.Time.Year(), Month: d.Time.Month(), Day: d.Time.Day()}
}

func domainDatePtr(d pgtype.Date) *domain.Date {
	if !d.Valid {
		return nil
	}
	v := domainDate(d)
	return &v
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// optional trims s and returns nil when nothing is left.
func optional(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
