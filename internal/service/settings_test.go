package service

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/domain"
)

func validSettings() Settings {
	return Settings{
		RTName: "RT 02", WindowOpen: domain.Clock{Hour: 22}, WindowClose: domain.Clock{Hour: 6},
		FinePerAbsence: 5000, MinPhotos: 3, MaxPhotos: 10,
	}
}

func TestSettingsValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Settings)
		wantKey string // "" = valid
	}{
		{"valid", func(*Settings) {}, ""},
		{"zero fine is allowed", func(s *Settings) { s.FinePerAbsence = 0 }, ""},
		{"min equals max", func(s *Settings) { s.MinPhotos, s.MaxPhotos = 5, 5 }, ""},
		{"max at limit", func(s *Settings) { s.MaxPhotos = 20 }, ""},
		{"empty name", func(s *Settings) { s.RTName = "  " }, "rt_name"},
		{"open equals close", func(s *Settings) { s.WindowOpen = s.WindowClose }, "window_open"},
		{"open before close", func(s *Settings) { s.WindowOpen = domain.Clock{Hour: 5} }, "window_open"},
		{"bad open clock", func(s *Settings) { s.WindowOpen = domain.Clock{Hour: 24} }, "window_open"},
		{"bad close clock", func(s *Settings) { s.WindowClose = domain.Clock{Min: 60} }, "window_close"},
		{"negative fine", func(s *Settings) { s.FinePerAbsence = -1 }, "fine_per_absence"},
		{"min below 1", func(s *Settings) { s.MinPhotos = 0 }, "min_photos"},
		{"max below min", func(s *Settings) { s.MaxPhotos = 2 }, "max_photos"},
		{"max above 20", func(s *Settings) { s.MaxPhotos = 21 }, "max_photos"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := validSettings()
			tc.mutate(&s)
			err := s.Validate()
			if tc.wantKey == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var ve ValidationErrors
			if !errors.As(err, &ve) || ve[tc.wantKey] == "" {
				t.Fatalf("want error on %q, got %v", tc.wantKey, err)
			}
		})
	}
}

func TestSettingsMapRoundTrip(t *testing.T) {
	live := domain.Date{Year: 2026, Month: 10, Day: 12}
	for _, s := range []Settings{validSettings(), func() Settings { s := validSettings(); s.GoLiveDate = &live; return s }()} {
		got, err := settingsFromMap(s.toMap())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, s) {
			t.Fatalf("got %+v want %+v", got, s)
		}
	}
}

func TestSettingsFromMapErrors(t *testing.T) {
	good := validSettings().toMap()
	for _, key := range []string{keyRTName, keyWindowOpen, keyWindowClose, keyFinePerAbsence, keyMinPhotos, keyMaxPhotos, keyGoLiveDate} {
		m := map[string]string{}
		for k, v := range good {
			if k != key {
				m[k] = v
			}
		}
		if _, err := settingsFromMap(m); err == nil {
			t.Errorf("missing %s: expected error", key)
		}
	}
	for key, bad := range map[string]string{
		keyWindowOpen: "25:00", keyFinePerAbsence: "abc", keyMaxPhotos: "", keyGoLiveDate: "not-a-date",
	} {
		m := validSettings().toMap()
		m[key] = bad
		if _, err := settingsFromMap(m); err == nil {
			t.Errorf("%s=%q: expected error", key, bad)
		}
	}
}

func TestValidationErrorsError(t *testing.T) {
	err := ValidationErrors{"b": "2", "a": "1"}.Error()
	if err != "invalid settings: a: 1; b: 2" {
		t.Fatalf("got %q", err)
	}
}

// Needs a migrated database (DATABASE_URL); skipped otherwise. It restores the original settings.
func TestSettingsServiceRoundTrip(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := NewSettingsService(pool)

	orig, err := svc.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Save(ctx, orig)

	changed := validSettings()
	changed.RTName, changed.FinePerAbsence = "RT 99", 7500
	if err := svc.Save(ctx, changed); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Load(ctx)
	if err != nil || !reflect.DeepEqual(got, changed) {
		t.Fatalf("got %+v err %v", got, err)
	}

	bad := changed
	bad.MaxPhotos = 99
	if err := svc.Save(ctx, bad); err == nil {
		t.Fatal("expected validation error")
	}
	if got, _ := svc.Load(ctx); got.MaxPhotos != changed.MaxPhotos {
		t.Fatal("invalid save must not change stored settings")
	}
}
