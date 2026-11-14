// Package job holds the background loops started by `patrolhub serve`.
package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/service"
)

const (
	retryAfter = 30 * time.Minute // after the first failed tick in a row
	minSleep   = time.Minute      // never spin, even if the next run is already due
)

// Closer closes finished patrol nights one minute after each window close, then sends
// the closed nights' reports to the WhatsApp group when Reports is set. Between runs it
// leaves the database alone so Neon can suspend the compute.
type Closer struct {
	Service  *service.ClosingService
	Reports  *service.ReportService // nil: no WhatsApp report
	MaxSleep time.Duration          // longest sleep between runs; defaults to six hours
}

func (c *Closer) tick(ctx context.Context, now time.Time) (next time.Time, failed bool) {
	res, err := c.Service.Tick(ctx, now)
	if err != nil {
		slog.Error("closer tick failed", "err", err)
		failed = true
	}
	if c.Reports != nil {
		if err := c.Reports.Tick(ctx, now); err != nil {
			slog.Error("whatsapp report tick failed", "err", err)
			failed = true
		}
	}
	return res.Next, failed
}

// nextSleep is how long to wait after a tick: until next (zero = unknown), at most limit,
// and at least minSleep. After consecutive failed ticks it retries sooner: retryAfter,
// doubling per further failure, so a night that keeps failing cannot keep Neon awake.
func nextSleep(now, next time.Time, failures int, limit time.Duration) time.Duration {
	d := limit
	if !next.IsZero() && next.Sub(now) < d {
		d = next.Sub(now)
	}
	if failures > 0 {
		retry := retryAfter
		for i := 1; i < failures && retry < limit; i++ {
			retry *= 2
		}
		d = min(d, retry)
	}
	return max(d, minSleep)
}

// Run ticks once immediately, then whenever the next run is due, until ctx is cancelled.
func (c *Closer) Run(ctx context.Context) {
	maxSleep := c.MaxSleep
	if maxSleep <= 0 {
		maxSleep = 6 * time.Hour
	}
	failures := 0
	for {
		now := time.Now()
		next, failed := c.tick(ctx, now)
		if failed {
			failures++
		} else {
			failures = 0
		}
		t := time.NewTimer(nextSleep(now, next, failures, maxSleep))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
