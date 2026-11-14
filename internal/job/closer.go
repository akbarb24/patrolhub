// Package job holds the background loops started by `patrolhub serve`.
package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/service"
)

const (
	retryAfter = 30 * time.Minute // after a failed tick
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
// at most retryAfter after a failure, and at least minSleep.
func nextSleep(now, next time.Time, failed bool, limit time.Duration) time.Duration {
	d := limit
	if !next.IsZero() && next.Sub(now) < d {
		d = next.Sub(now)
	}
	if failed && d > retryAfter {
		d = retryAfter
	}
	return max(d, minSleep)
}

// Run ticks once immediately, then whenever the next run is due, until ctx is cancelled.
func (c *Closer) Run(ctx context.Context) {
	maxSleep := c.MaxSleep
	if maxSleep <= 0 {
		maxSleep = 6 * time.Hour
	}
	for {
		now := time.Now()
		next, failed := c.tick(ctx, now)
		t := time.NewTimer(nextSleep(now, next, failed, maxSleep))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
