package domain

import (
	"fmt"
	"github.com/akbarbinaji/patrolhub/internal/config"
	"time"
)

// Clock is a wall-clock time of day such as 22:00.
type Clock struct{ Hour, Min int }

// ParseClock parses "HH:MM".
func ParseClock(s string) (Clock, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return Clock{}, fmt.Errorf("invalid clock %q: %w", s, err)
	}
	return Clock{t.Hour(), t.Minute()}, nil
}

func (c Clock) String() string { return fmt.Sprintf("%02d:%02d", c.Hour, c.Min) }

func (c Clock) minutes() int { return c.Hour*60 + c.Min }

type WindowState struct {
	Open      bool
	NightDate Date // the running night when Open; the upcoming night otherwise
	OpensAt   time.Time
	ClosesAt  time.Time
}

// CurrentWindow decides which night a moment belongs to. The window of night D
// opens on D at openAt and closes on D+1 at closeAt (openAt > closeAt, so it
// crosses midnight). The result does not depend on now's time zone.
func CurrentWindow(now time.Time, openAt, closeAt Clock) WindowState {
	local := now.In(config.Jakarta)
	today := DateOf(local)
	mins := local.Hour()*60 + local.Minute()

	var w WindowState
	switch {
	case mins >= openAt.minutes():
		w.Open, w.NightDate = true, today
	case mins < closeAt.minutes():
		w.Open, w.NightDate = true, today.AddDays(-1)
	default:
		w.NightDate = today
	}
	w.OpensAt = w.NightDate.At(openAt.Hour, openAt.Min)
	w.ClosesAt = ClosesAt(w.NightDate, closeAt)
	return w
}

// ClosesAt is the moment the window of the given night ends.
func ClosesAt(night Date, closeAt Clock) time.Time {
	return night.AddDays(1).At(closeAt.Hour, closeAt.Min)
}
