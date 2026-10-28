// Package job holds the background loops started by `patrolhub serve`.
package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/service"
)

// Closer closes finished patrol nights every five minutes, then sends the closed
// nights' reports to the WhatsApp group when Reports is set.
type Closer struct {
	Service  *service.ClosingService
	Reports  *service.ReportService // nil: no WhatsApp report
	Interval time.Duration          // defaults to five minutes
}

func (c *Closer) tick(ctx context.Context, now time.Time) {
	if _, err := c.Service.Tick(ctx, now); err != nil {
		slog.Error("closer tick failed", "err", err)
	}
	if c.Reports != nil {
		if err := c.Reports.Tick(ctx, now); err != nil {
			slog.Error("whatsapp report tick failed", "err", err)
		}
	}
}

// Run ticks once immediately, then on every interval until ctx is cancelled.
func (c *Closer) Run(ctx context.Context) {
	interval := c.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	c.tick(ctx, time.Now())
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			c.tick(ctx, now)
		}
	}
}
