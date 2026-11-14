package job

import (
	"testing"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/config"
)

func TestNextSleep(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, config.Jakarta)
	const limit = 6 * time.Hour
	cases := []struct {
		name     string
		next     time.Time
		failures int
		want     time.Duration
	}{
		{"until next run", now.Add(2 * time.Hour), 0, 2 * time.Hour},
		{"capped at limit", now.Add(20 * time.Hour), 0, limit},
		{"no next run known", time.Time{}, 0, limit},
		{"next run in the past never spins", now.Add(-time.Minute), 0, time.Minute},
		{"failure retries sooner", now.Add(2 * time.Hour), 1, 30 * time.Minute},
		{"failure keeps an earlier run", now.Add(10 * time.Minute), 1, 10 * time.Minute},
		{"second failure backs off", now.Add(20 * time.Hour), 2, time.Hour},
		{"fourth failure backs off", now.Add(20 * time.Hour), 4, 4 * time.Hour},
		{"persistent failure stops at limit", time.Time{}, 30, limit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextSleep(now, tc.next, tc.failures, limit); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
