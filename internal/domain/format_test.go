package domain

import (
	"testing"
	"time"
)

func TestDisplayName(t *testing.T) {
	s := func(v string) *string { return &v }
	tests := []struct {
		name  *string
		block string
		want  string
	}{
		{s("Dhani"), "A-2", "Dhani (A-2)"},
		{nil, "C-10", "C-10"},
		{s("  "), "C-10", "C-10"},
		{s(" Dhani "), " A-2 ", "Dhani (A-2)"},
	}
	for _, tc := range tests {
		if got := DisplayName(tc.name, tc.block); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
}

func TestFormatRupiah(t *testing.T) {
	tests := map[int]string{
		0: "Rp 0", 5000: "Rp 5.000", 180000: "Rp 180.000", 1250000: "Rp 1.250.000",
		999: "Rp 999", -5000: "-Rp 5.000",
	}
	for n, want := range tests {
		if got := FormatRupiah(n); got != want {
			t.Errorf("%d: got %q want %q", n, got, want)
		}
	}
}

func TestFormatDateID(t *testing.T) {
	if got := FormatDateID(Date{2026, 10, 7}); got != "Rabu, 7 Oktober 2026" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatShortDateID(t *testing.T) {
	for _, tc := range []struct {
		d    Date
		want string
	}{
		{Date{2026, 10, 28}, "Rabu, 28 Okt 2026"},
		{Date{2026, 1, 1}, "Kamis, 1 Jan 2026"},
		{Date{2026, 8, 17}, "Senin, 17 Agu 2026"},
		{Date{2026, 12, 31}, "Kamis, 31 Des 2026"},
	} {
		if got := FormatShortDateID(tc.d); got != tc.want {
			t.Errorf("FormatShortDateID(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestNames(t *testing.T) {
	if MonthNameID(time.September) != "September" || MonthNameID(0) != "" {
		t.Error("month names")
	}
	if WeekdayNameID(1) != "Senin" || WeekdayNameID(7) != "Minggu" || WeekdayNameID(8) != "" {
		t.Error("weekday names")
	}
}
