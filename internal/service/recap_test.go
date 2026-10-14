package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
)

func TestCheckMonth(t *testing.T) {
	today := domain.Date{Year: 2026, Month: 10, Day: 14}
	tests := []struct {
		year  int
		month time.Month
		want  error
	}{
		{2026, 10, nil}, {2026, 9, nil}, {2025, 12, nil},
		{2026, 11, ErrFutureMonth}, {2027, 1, ErrFutureMonth},
		{1999, 12, ErrNotFound}, {2101, 1, ErrNotFound}, {2026, 13, ErrNotFound}, {2026, 0, ErrNotFound},
	}
	for _, tc := range tests {
		if got := checkMonth(tc.year, tc.month, today); !errors.Is(got, tc.want) {
			t.Errorf("%d-%d: got %v want %v", tc.year, tc.month, got, tc.want)
		}
	}
}

func TestRecapUsesClosedNightsWithATeamOnly(t *testing.T) {
	e := newNightEnv(t) // Wed 7 Oct closed: Anton present, Budi and C-3 absent
	ctx := context.Background()
	rs := NewRecapService(e.pool, e.settings)
	today := domain.Date{Year: 2026, Month: 10, Day: 14}

	// An open night (Wed 14 Oct is still running) and a night nobody was scheduled for are not part of it.
	q := db.New(e.pool)
	q.InsertNightIfMissing(ctx, pgDate(domain.Date{Year: 2026, Month: 10, Day: 14}))
	q.InsertNightIfMissing(ctx, pgDate(domain.Date{Year: 2026, Month: 10, Day: 13}))
	n, _ := q.GetNightByDate(ctx, pgDate(domain.Date{Year: 2026, Month: 10, Day: 13}))
	q.CloseNight(ctx, db.CloseNightParams{ID: n.ID, Status: "NO_REPORT", ClosedAt: pgTime(testNow)})

	r, err := rs.Month(ctx, 2026, time.October, today)
	if err != nil {
		t.Fatal(err)
	}
	s := r.Summary
	if s.NightsTotal != 1 || s.Reported != 1 || s.NoReport != 0 || s.PresentCount != 1 || s.SlotCount != 3 || s.RatePercent != 33 ||
		s.TotalFine != 10000 || s.FinedMembers != 2 {
		t.Fatalf("%+v", s)
	}
	if r.RTName != "RT 02" || r.FineRate != 5000 || len(r.Timeline) != 1 || r.Timeline[0].Date != wedNight {
		t.Fatalf("%+v", r)
	}
	wed := r.Cards[2]
	if len(wed.Dates) != 1 || len(wed.Rows) != 3 || wed.Subtotal != 10000 {
		t.Fatalf("wednesday card %+v", wed)
	}

	if _, err := rs.Month(ctx, 2026, time.November, today); !errors.Is(err, ErrFutureMonth) {
		t.Fatalf("future month: %v", err)
	}
	empty, err := rs.Month(ctx, 2026, time.September, today)
	if err != nil || empty.Summary.NightsTotal != 0 {
		t.Fatalf("september %+v %v", empty.Summary, err)
	}
}
