package job

import (
	"context"
	"log/slog"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/service"
)

// Retention removes expired photos once a day.
type Retention struct {
	Service  *service.RetentionService
	Interval time.Duration // defaults to 24 hours
}

func (r *Retention) tick(ctx context.Context, now time.Time) {
	if _, err := r.Service.Run(ctx, now); err != nil {
		slog.Error("retention run failed", "err", err)
	}
}

// Run works once at start, then on every interval until ctx is cancelled.
func (r *Retention) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	r.tick(ctx, time.Now())
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			r.tick(ctx, now)
		}
	}
}
