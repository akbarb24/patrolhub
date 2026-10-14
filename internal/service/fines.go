package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
)

type FineRow struct {
	MemberID    int64
	DisplayName string
	Total       int
	Paid        int
	Remaining   int // negative when overpaid
}

type PaymentView struct {
	ID          int64
	MemberID    int64
	DisplayName string
	Amount      int
	PaidAt      domain.Date
}

type FineMonth struct {
	Year       int
	Month      time.Month
	Rows       []FineRow // members with fines or payments that month
	Payments   []PaymentView
	TotalFines int
	TotalPaid  int
}

type PaymentInput struct {
	MemberID int64
	Year     int
	Month    time.Month
	Amount   int
	PaidAt   domain.Date
}

type FineService struct {
	pool *pgxpool.Pool
}

func NewFineService(pool *pgxpool.Pool) *FineService { return &FineService{pool: pool} }

func period(year int, month time.Month) string { return fmt.Sprintf("%04d-%02d", year, int(month)) }

// monthRange returns the first and last day of a calendar month.
func monthRange(year int, month time.Month) (domain.Date, domain.Date) {
	first := domain.Date{Year: year, Month: month, Day: 1}
	next := time.Date(year, month+1, 1, 12, 0, 0, 0, time.UTC)
	return first, domain.Date{Year: next.Year(), Month: next.Month(), Day: 1}.AddDays(-1)
}

// Month totals the fines of closed nights per member and subtracts recorded payments.
func (s *FineService) Month(ctx context.Context, year int, month time.Month) (FineMonth, error) {
	q := db.New(s.pool)
	first, last := monthRange(year, month)
	att, err := q.ListAttendanceBetween(ctx, db.ListAttendanceBetweenParams{FromDate: pgDate(first), ToDate: pgDate(last)})
	if err != nil {
		return FineMonth{}, err
	}
	pays, err := q.ListPaymentsForPeriod(ctx, period(year, month))
	if err != nil {
		return FineMonth{}, err
	}

	rows := map[int64]*FineRow{}
	names := map[int64]string{}
	row := func(id int64) *FineRow {
		r, ok := rows[id]
		if !ok {
			r = &FineRow{MemberID: id, DisplayName: names[id]}
			rows[id] = r
		}
		return r
	}
	for _, a := range att {
		names[a.MemberID] = domain.DisplayName(a.MemberName, a.HouseBlock)
		if a.ClosedAt.Valid && a.FineAmount > 0 {
			row(a.MemberID).Total += int(a.FineAmount)
		}
	}
	labels, err := memberLabels(ctx, q)
	if err != nil {
		return FineMonth{}, err
	}

	fm := FineMonth{Year: year, Month: month}
	for _, p := range pays {
		names[p.MemberID] = labels[p.MemberID]
		row(p.MemberID).Paid += int(p.Amount)
		fm.Payments = append(fm.Payments, PaymentView{ID: p.ID, MemberID: p.MemberID, DisplayName: labels[p.MemberID], Amount: int(p.Amount), PaidAt: domainDate(p.PaidAt)})
	}
	for _, r := range rows {
		if r.DisplayName == "" {
			r.DisplayName = labels[r.MemberID]
		}
		r.Remaining = r.Total - r.Paid
		fm.TotalFines += r.Total
		fm.TotalPaid += r.Paid
		fm.Rows = append(fm.Rows, *r)
	}
	sort.Slice(fm.Rows, func(i, j int) bool { return fm.Rows[i].DisplayName < fm.Rows[j].DisplayName })
	return fm, nil
}

// RecordPayment stores a payment for a member's fines of one month.
func (s *FineService) RecordPayment(ctx context.Context, adminID int64, in PaymentInput) error {
	if in.Amount <= 0 {
		return ValidationErrors{"amount": "Nominal harus lebih dari 0."}
	}
	return WithTx(ctx, s.pool, func(q *db.Queries) error {
		if _, err := q.GetMember(ctx, in.MemberID); err != nil {
			return ValidationErrors{"member_id": "Peserta tidak ditemukan."}
		}
		p, err := q.InsertPayment(ctx, db.InsertPaymentParams{
			MemberID: in.MemberID, Period: period(in.Year, in.Month), Amount: int32(in.Amount), PaidAt: pgDate(in.PaidAt), RecordedBy: adminID,
		})
		if err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]any{"member_id": in.MemberID, "period": p.Period, "amount": in.Amount})
		return q.InsertAudit(ctx, db.InsertAuditParams{
			Entity: "fine_payment", EntityID: p.ID, Action: "record_payment", Actor: adminActor(adminID), Detail: detail,
		})
	})
}

func (s *FineService) DeletePayment(ctx context.Context, adminID, id int64) error {
	return WithTx(ctx, s.pool, func(q *db.Queries) error {
		p, err := q.GetPayment(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if err := q.DeletePayment(ctx, id); err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]any{"member_id": p.MemberID, "period": p.Period, "amount": p.Amount})
		return q.InsertAudit(ctx, db.InsertAuditParams{
			Entity: "fine_payment", EntityID: id, Action: "delete_payment", Actor: adminActor(adminID), Detail: detail,
		})
	})
}
