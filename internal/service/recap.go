package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
)

// ErrFutureMonth is returned for months after the current one (and absurd years).
var ErrFutureMonth = errors.New("month is in the future")

// Recap is a month's public recap plus what the page header needs.
type Recap struct {
	domain.MonthRecap
	RTName   string
	FineRate int // current rate per absence, shown as a hint
}

type RecapService struct {
	pool     *pgxpool.Pool
	settings *SettingsService
}

func NewRecapService(pool *pgxpool.Pool, settings *SettingsService) *RecapService {
	return &RecapService{pool: pool, settings: settings}
}

// checkMonth rejects months that have not started yet.
func checkMonth(year int, month time.Month, today domain.Date) error {
	if year < 2000 || year > 2100 || month < 1 || month > 12 {
		return ErrNotFound
	}
	cur := domain.Date{Year: today.Year, Month: today.Month, Day: 1}
	if (domain.Date{Year: year, Month: month, Day: 1}).Compare(cur) > 0 {
		return ErrFutureMonth
	}
	return nil
}

// NightRows returns the closed nights of a month with their attendance. Nights that
// had no team (nobody was on duty) are left out: there was nothing to report.
func (s *RecapService) NightRows(ctx context.Context, year int, month time.Month) ([]domain.NightRow, error) {
	q := db.New(s.pool)
	first, last := monthRange(year, month)
	nights, err := q.ListNightsBetween(ctx, db.ListNightsBetweenParams{FromDate: pgDate(first), ToDate: pgDate(last)})
	if err != nil {
		return nil, err
	}
	att, err := q.ListAttendanceBetween(ctx, db.ListAttendanceBetweenParams{FromDate: pgDate(first), ToDate: pgDate(last)})
	if err != nil {
		return nil, err
	}

	byNight := map[int64][]domain.AttendanceRow{}
	for _, a := range att {
		byNight[a.NightID] = append(byNight[a.NightID], domain.AttendanceRow{
			MemberID: a.MemberID, DisplayName: domain.DisplayName(a.MemberName, a.HouseBlock),
			Status: domain.AttendanceStatus(a.Status), Fine: int(a.FineAmount),
		})
	}
	var rows []domain.NightRow
	for _, n := range nights {
		if !n.ClosedAt.Valid || len(byNight[n.ID]) == 0 {
			continue
		}
		rows = append(rows, domain.NightRow{
			Date: domainDate(n.PatrolDate), Status: domain.NightStatus(n.Status), Note: n.SituationNote, Attendance: byNight[n.ID],
		})
	}
	return rows, nil
}

// Month builds the recap of a calendar month; today decides which months exist yet.
func (s *RecapService) Month(ctx context.Context, year int, month time.Month, today domain.Date) (Recap, error) {
	if err := checkMonth(year, month, today); err != nil {
		return Recap{}, err
	}
	set, err := s.settings.Load(ctx)
	if err != nil {
		return Recap{}, err
	}
	rows, err := s.NightRows(ctx, year, month)
	if err != nil {
		return Recap{}, err
	}
	return Recap{MonthRecap: domain.BuildMonthRecap(year, month, rows), RTName: set.RTName, FineRate: set.FinePerAbsence}, nil
}
