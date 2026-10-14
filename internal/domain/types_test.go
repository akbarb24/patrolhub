package domain

import (
	"testing"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/config"
)

func TestDateOfUsesJakarta(t *testing.T) {
	// 2026-10-07 20:00 UTC is already 2026-10-08 03:00 in Jakarta.
	got := DateOf(time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC))
	if got != (Date{2026, 10, 8}) {
		t.Fatalf("got %v", got)
	}
}

func TestDateISOWeekday(t *testing.T) {
	tests := []struct {
		date string
		want int
	}{
		{"2026-10-05", 1}, // Monday
		{"2026-10-07", 3},
		{"2026-10-11", 7}, // Sunday
	}
	for _, tc := range tests {
		d, err := ParseDate(tc.date)
		if err != nil {
			t.Fatal(err)
		}
		if got := d.ISOWeekday(); got != tc.want {
			t.Errorf("%s: got %d want %d", tc.date, got, tc.want)
		}
	}
}

func TestDateAddDaysAndString(t *testing.T) {
	tests := []struct {
		from string
		n    int
		want string
	}{
		{"2026-10-07", 1, "2026-10-08"},
		{"2026-10-31", 1, "2026-11-01"},
		{"2026-01-01", -1, "2025-12-31"},
		{"2028-02-28", 1, "2028-02-29"},
	}
	for _, tc := range tests {
		d, _ := ParseDate(tc.from)
		if got := d.AddDays(tc.n).String(); got != tc.want {
			t.Errorf("%s%+d: got %s want %s", tc.from, tc.n, got, tc.want)
		}
	}
}

func TestDateAtAndCompare(t *testing.T) {
	d := Date{2026, 10, 7}
	at := d.At(22, 30)
	if at.Location() != config.Jakarta || at.Hour() != 22 || at.Minute() != 30 {
		t.Fatalf("unexpected %v", at)
	}
	if d.Compare(Date{2026, 10, 8}) != -1 || d.Compare(d) != 0 ||
		d.Compare(Date{2026, 9, 30}) != 1 || d.Compare(Date{2025, 12, 31}) != 1 ||
		d.Compare(Date{2026, 11, 1}) != -1 {
		t.Fatal("Compare returned wrong ordering")
	}
}

func TestParseDateInvalid(t *testing.T) {
	if _, err := ParseDate("2026-13-01"); err == nil {
		t.Fatal("expected error")
	}
}
