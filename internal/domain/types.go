// Package domain holds PatrolHub's business rules as pure functions: no database,
// no HTTP, and no reading of the clock. The current time is always a parameter.
package domain

import (
	"fmt"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/config"
)

type NightStatus string

const (
	NightOpen          NightStatus = "OPEN"
	NightReported      NightStatus = "REPORTED"
	NightNoReport      NightStatus = "NO_REPORT"
	NightRainRequested NightStatus = "RAIN_REQUESTED"
	NightRainedOut     NightStatus = "RAINED_OUT"
)

type AttendanceStatus string

const (
	Present   AttendanceStatus = "PRESENT"
	Absent    AttendanceStatus = "ABSENT"
	RainedOut AttendanceStatus = "RAINED_OUT"
)

type AttendanceSource string

const (
	SourceScheduled AttendanceSource = "SCHEDULED"
	SourceOverride  AttendanceSource = "OVERRIDE"
	SourceWalkIn    AttendanceSource = "WALK_IN"
)

// Date is a calendar date without a time of day, always interpreted in Asia/Jakarta.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf returns the calendar date of t in Asia/Jakarta.
func DateOf(t time.Time) Date {
	y, m, d := t.In(config.Jakarta).Date()
	return Date{y, m, d}
}

// ParseDate parses "YYYY-MM-DD".
func ParseDate(s string) (Date, error) {
	t, err := time.ParseInLocation("2006-01-02", s, config.Jakarta)
	if err != nil {
		return Date{}, err
	}
	return DateOf(t), nil
}

// At returns the given wall-clock time on this date in Asia/Jakarta.
func (d Date) At(hour, min int) time.Time {
	return time.Date(d.Year, d.Month, d.Day, hour, min, 0, 0, config.Jakarta)
}

func (d Date) AddDays(n int) Date {
	return DateOf(time.Date(d.Year, d.Month, d.Day+n, 12, 0, 0, 0, config.Jakarta))
}

// ISOWeekday returns 1=Monday ... 7=Sunday.
func (d Date) ISOWeekday() int {
	wd := time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, config.Jakarta).Weekday()
	return (int(wd)+6)%7 + 1
}

// Compare returns -1, 0 or +1.
func (d Date) Compare(o Date) int {
	switch {
	case d.Year != o.Year:
		return cmpInt(d.Year, o.Year)
	case d.Month != o.Month:
		return cmpInt(int(d.Month), int(o.Month))
	default:
		return cmpInt(d.Day, o.Day)
	}
}

func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
