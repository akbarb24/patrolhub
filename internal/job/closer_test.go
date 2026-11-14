package job

import (
	"testing"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/config"
)

func TestNextSleep(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, config.Jakarta)
	const max = 6 * time.Hour
	cases := []struct {
		name   string
		next   time.Time
		failed bool
		want   time.Duration
	}{
		{"until next run", now.Add(2 * time.Hour), false, 2 * time.Hour},
		{"capped at max", now.Add(20 * time.Hour), false, max},
		{"no next run known", time.Time{}, false, max},
		{"next run in the past never spins", now.Add(-time.Minute), false, time.Minute},
		{"failure retries sooner", now.Add(2 * time.Hour), true, 30 * time.Minute},
		{"failure keeps an earlier run", now.Add(10 * time.Minute), true, 10 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextSleep(now, tc.next, tc.failed, max); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
