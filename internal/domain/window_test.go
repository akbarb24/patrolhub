package domain

import (
	"testing"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/config"
)

func wib(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, config.Jakarta)
}

func TestCurrentWindow(t *testing.T) {
	open, closeAt := Clock{22, 0}, Clock{6, 0}
	tests := []struct {
		name      string
		now       time.Time
		wantOpen  bool
		wantNight Date
	}{
		{"before open", wib(2026, 10, 7, 21, 59), false, Date{2026, 10, 7}},
		{"at open", wib(2026, 10, 7, 22, 0), true, Date{2026, 10, 7}},
		{"late evening", wib(2026, 10, 7, 23, 59), true, Date{2026, 10, 7}},
		{"midnight", wib(2026, 10, 8, 0, 0), true, Date{2026, 10, 7}},
		{"just before close", wib(2026, 10, 8, 5, 59), true, Date{2026, 10, 7}},
		{"at close", wib(2026, 10, 8, 6, 0), false, Date{2026, 10, 8}},
		{"noon", wib(2026, 10, 8, 12, 0), false, Date{2026, 10, 8}},
		{"UTC input", time.Date(2026, 10, 7, 15, 30, 0, 0, time.UTC), true, Date{2026, 10, 7}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := CurrentWindow(tc.now, open, closeAt)
			if w.Open != tc.wantOpen || w.NightDate != tc.wantNight {
				t.Fatalf("got open=%v night=%v", w.Open, w.NightDate)
			}
			if !w.OpensAt.Equal(tc.wantNight.At(22, 0)) {
				t.Errorf("OpensAt %v", w.OpensAt)
			}
			if !w.ClosesAt.Equal(tc.wantNight.AddDays(1).At(6, 0)) {
				t.Errorf("ClosesAt %v", w.ClosesAt)
			}
		})
	}
}

func TestClosesAt(t *testing.T) {
	if got := ClosesAt(Date{2026, 10, 31}, Clock{6, 0}); !got.Equal(wib(2026, 11, 1, 6, 0)) {
		t.Fatalf("got %v", got)
	}
}

func TestParseClock(t *testing.T) {
	c, err := ParseClock("22:05")
	if err != nil || c != (Clock{22, 5}) || c.String() != "22:05" {
		t.Fatalf("got %v %v", c, err)
	}
	for _, bad := range []string{"", "25:00", "22", "22:60", "ab:cd"} {
		if _, err := ParseClock(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}
