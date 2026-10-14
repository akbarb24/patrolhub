package domain

import (
	"strconv"
	"strings"
	"time"
)

var weekdayID = [...]string{"Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu", "Minggu"}

var monthID = [...]string{
	"Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember",
}

// WeekdayNameID returns the Indonesian weekday name for an ISO weekday (1=Senin).
func WeekdayNameID(iso int) string {
	if iso < 1 || iso > 7 {
		return ""
	}
	return weekdayID[iso-1]
}

// MonthNameID returns the Indonesian month name.
func MonthNameID(m time.Month) string {
	if m < 1 || m > 12 {
		return ""
	}
	return monthID[m-1]
}

// DisplayName renders "Name (Block)", or just the block when the name is empty.
func DisplayName(name *string, block string) string {
	block = strings.TrimSpace(block)
	if name == nil || strings.TrimSpace(*name) == "" {
		return block
	}
	return strings.TrimSpace(*name) + " (" + block + ")"
}

// FormatRupiah renders an amount as "Rp 1.250.000".
func FormatRupiah(n int) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return sign + "Rp " + b.String()
}

// FormatDateID renders e.g. "Rabu, 7 Oktober 2026".
func FormatDateID(d Date) string {
	return WeekdayNameID(d.ISOWeekday()) + ", " + strconv.Itoa(d.Day) + " " + MonthNameID(d.Month) + " " + strconv.Itoa(d.Year)
}
