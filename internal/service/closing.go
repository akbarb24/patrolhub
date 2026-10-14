package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
)

// backfillDays is how far back the closer looks for nights nobody ever opened.
const backfillDays = 14

// TickResult counts what one run of the closer did.
type TickResult struct {
	Created int // nights that had no row (nobody opened the page) and were created
	Closed  int
}

// ClosingService creates missed nights and closes finished ones. Running it twice
// in a row changes nothing the second time.
type ClosingService struct {
	pool     *pgxpool.Pool
	settings *SettingsService
}

func NewClosingService(pool *pgxpool.Pool, settings *SettingsService) *ClosingService {
	return &ClosingService{pool: pool, settings: settings}
}

// Tick does one pass. A failure on one night is logged and does not stop the others;
// the errors of all failed nights are returned together.
func (s *ClosingService) Tick(ctx context.Context, now time.Time) (TickResult, error) {
	var res TickResult
	set, err := s.settings.Load(ctx)
	if err != nil {
		return res, err
	}
	if set.GoLiveDate == nil {
		return res, nil // the app is not live yet
	}
	goLive := *set.GoLiveDate
	today := domain.DateOf(now)
	ended := func(d domain.Date) bool { return !domain.ClosesAt(d, set.WindowClose).After(now) }

	var errs []error
	fail := func(d domain.Date, what string, err error) {
		slog.Error("closer: "+what, "night", d.String(), "err", err)
		errs = append(errs, fmt.Errorf("%s %s: %w", what, d, err))
	}

	// 1. Create the nights (that have a team) whose page nobody opened, so they can be closed and fined.
	start := goLive
	if recent := today.AddDays(-backfillDays); recent.Compare(start) > 0 {
		start = recent
	}
	for d := start; d.Compare(today) <= 0; d = d.AddDays(1) {
		if !ended(d) {
			continue
		}
		created, err := s.createIfMissing(ctx, d)
		if err != nil {
			fail(d, "create night", err)
		} else if created {
			res.Created++
		}
	}

	// 2. Close every finished night that is still open.
	nights, err := db.New(s.pool).ListUnclosedNightsBefore(ctx, pgDate(today.AddDays(1)))
	if err != nil {
		return res, errors.Join(append(errs, err)...)
	}
	for _, n := range nights {
		d := domainDate(n.PatrolDate)
		if d.Compare(goLive) < 0 || !ended(d) {
			continue
		}
		closed, err := s.closeNight(ctx, d, set, now)
		if err != nil {
			fail(d, "close night", err)
		} else if closed {
			res.Closed++
		}
	}

	if res.Created > 0 || res.Closed > 0 {
		slog.Info("closer finished", "created", res.Created, "closed", res.Closed)
	}
	return res, errors.Join(errs...)
}

func (s *ClosingService) createIfMissing(ctx context.Context, d domain.Date) (created bool, err error) {
	err = WithTx(ctx, s.pool, func(q *db.Queries) error {
		if _, err := q.GetNightByDate(ctx, pgDate(d)); err == nil {
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// A weekday nobody is scheduled for has no patrol night; creating one would
		// only show up as a bogus "no report" in the recap.
		team, err := resolveTeam(ctx, q, d)
		if err != nil || len(team) == 0 {
			return err
		}
		night, err := openNight(ctx, q, d)
		if err != nil {
			return err
		}
		created = true
		return syncTeam(ctx, q, night)
	})
	return created, err
}

func (s *ClosingService) closeNight(ctx context.Context, d domain.Date, set Settings, now time.Time) (closed bool, err error) {
	err = WithTx(ctx, s.pool, func(q *db.Queries) error {
		night, err := q.GetNightByDateForUpdate(ctx, pgDate(d))
		if err != nil {
			return err
		}
		if night.ClosedAt.Valid {
			return nil // somebody else closed it meanwhile
		}
		if err := syncTeam(ctx, q, night); err != nil {
			return err
		}

		rows, err := q.ListAttendanceByNight(ctx, night.ID)
		if err != nil {
			return err
		}
		current := make(map[int64]domain.AttendanceStatus, len(rows))
		for _, r := range rows {
			current[r.MemberID] = domain.AttendanceStatus(r.Status)
		}
		photos, err := q.CountPhotosByNight(ctx, night.ID)
		if err != nil {
			return err
		}

		res := domain.CloseNight(domain.CloseInput{
			Status:     domain.NightStatus(night.Status),
			HasReport:  night.FirstReportedAt.Valid,
			PhotoCount: int(photos),
			MinPhotos:  set.MinPhotos,
			FineRate:   set.FinePerAbsence,
			Attendance: current,
		})

		totalFine := 0
		for id, status := range res.Attendance {
			if status != current[id] {
				if err := q.SetAttendanceStatus(ctx, db.SetAttendanceStatusParams{NightID: night.ID, MemberID: id, Status: string(status)}); err != nil {
					return err
				}
			}
			fine := res.Fines[id]
			totalFine += fine
			if err := q.SetAttendanceFine(ctx, db.SetAttendanceFineParams{NightID: night.ID, MemberID: id, FineAmount: int32(fine)}); err != nil {
				return err
			}
		}

		rate := int32(set.FinePerAbsence)
		err = q.CloseNight(ctx, db.CloseNightParams{
			ID: night.ID, Status: string(res.Status), FineRate: &rate, NeedsReview: res.NeedsReview, ClosedAt: pgTime(now),
		})
		if err != nil {
			return err
		}

		detail, _ := json.Marshal(map[string]any{
			"night_id": night.ID, "status": res.Status, "needs_review": res.NeedsReview,
			"photos": photos, "fine_rate": rate, "total_fine": totalFine, "members": len(rows),
		})
		closed = true
		return q.InsertAudit(ctx, db.InsertAuditParams{
			Entity: "patrol_night", EntityID: night.ID, Action: "close_night", Actor: "SYSTEM", Detail: detail,
		})
	})
	return closed, err
}
