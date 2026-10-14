package service

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/storage"
)

// PhotoRetentionDays is about six months.
const PhotoRetentionDays = 183

// RetentionService deletes photos once they are older than the retention period.
type RetentionService struct {
	pool  *pgxpool.Pool
	store storage.PhotoStore
}

func NewRetentionService(pool *pgxpool.Pool, store storage.PhotoStore) *RetentionService {
	return &RetentionService{pool: pool, store: store}
}

// Run deletes the file and then the row of every photo older than 183 days. A file that
// is already gone still lets the row go; any other failure keeps the row so the next
// run retries. It returns how many photos were removed.
func (s *RetentionService) Run(ctx context.Context, now time.Time) (int, error) {
	q := db.New(s.pool)
	old, err := q.ListPhotosOlderThan(ctx, pgTime(now.AddDate(0, 0, -PhotoRetentionDays)))
	if err != nil {
		return 0, err
	}
	removed := 0
	var errs []error
	for _, p := range old {
		if err := s.store.Delete(ctx, p.StorageKey); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Error("retention: delete photo file", "photo", p.ID, "key", p.StorageKey, "err", err)
			errs = append(errs, err)
			continue
		}
		if err := q.DeletePhoto(ctx, p.ID); err != nil {
			slog.Error("retention: delete photo row", "photo", p.ID, "err", err)
			errs = append(errs, err)
			continue
		}
		removed++
	}
	if removed > 0 {
		slog.Info("retention finished", "removed", removed)
	}
	return removed, errors.Join(errs...)
}
