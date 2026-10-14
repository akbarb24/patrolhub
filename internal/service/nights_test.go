package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
)

type nightEnv struct {
	*closeEnv
	nights  *NightService
	fineSvc *FineService
	adminID int64
}

// newNightEnv has Wednesday 7 Oct closed: Anton present, Budi and C-3 absent (5,000 each), 3 photos.
func newNightEnv(t *testing.T) *nightEnv {
	t.Helper()
	e := &nightEnv{closeEnv: newCloseEnv(t, "2026-10-01")}
	e.nights = NewNightService(e.pool, e.settings, e.store)
	e.fineSvc = NewFineService(e.pool)
	admin, err := NewAdminService(e.pool).Create(context.Background(), "admin", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	e.adminID = admin.ID
	e.patrol.ToggleAttendance(context.Background(), wed2230, e.members["anton"].ID, true, rc)
	e.addPhotos(wedNight, 3)
	e.tick(afterWed)
	return e
}

func (e *nightEnv) auditActions() []string {
	log, err := db.New(e.pool).ListAuditByNight(context.Background(), e.night(wedNight).ID)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, l := range log {
		out = append(out, l.Actor+":"+l.Action)
	}
	return out
}

func TestMarkRainAfterCloseRemovesFines(t *testing.T) {
	e := newNightEnv(t)
	ctx := context.Background()
	if err := e.nights.MarkRain(ctx, e.adminID, wedNight); err != nil {
		t.Fatal(err)
	}
	n := e.night(wedNight)
	if n.Status != "RAINED_OUT" || n.NeedsReview || !n.ClosedAt.Valid {
		t.Fatalf("%+v", n)
	}
	for who, v := range e.fines(wedNight) {
		if v != "RAINED_OUT/0" {
			t.Errorf("%s: %s", who, v)
		}
	}
	if got := e.auditActions()[len(e.auditActions())-1]; got != "admin:1:mark_rain" {
		t.Fatalf("audit %s", got)
	}
	if err := e.nights.MarkRain(ctx, e.adminID, wedNight); err != nil {
		t.Fatalf("marking twice must be harmless: %v", err)
	}
	if err := e.nights.MarkRain(ctx, e.adminID, wedNight.AddDays(1)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown night: %v", err)
	}

	// The fines page now shows nothing for that month.
	fm, _ := e.fineSvc.Month(ctx, 2026, time.October)
	if fm.TotalFines != 0 || len(fm.Rows) != 0 {
		t.Fatalf("%+v", fm)
	}
}

func TestMarkRainBeforeClose(t *testing.T) {
	e := newCloseEnv(t, "2026-10-01")
	ctx := context.Background()
	ns := NewNightService(e.pool, e.settings, e.store)
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["anton"].ID, true, rc)
	if err := ns.MarkRain(ctx, 1, wedNight); err != nil {
		t.Fatal(err)
	}
	if n := e.night(wedNight); n.Status != "RAINED_OUT" || n.ClosedAt.Valid {
		t.Fatalf("%+v", n)
	}
	e.tick(afterWed)
	for who, v := range e.fines(wedNight) {
		if v != "RAINED_OUT/0" {
			t.Errorf("%s: %s", who, v)
		}
	}
}

func rainRequestedAndClosed(t *testing.T, photos int) *nightEnv {
	t.Helper()
	e := &nightEnv{closeEnv: newCloseEnv(t, "2026-10-01")}
	e.nights = NewNightService(e.pool, e.settings, e.store)
	e.fineSvc = NewFineService(e.pool)
	ctx := context.Background()
	admin, _ := NewAdminService(e.pool).Create(ctx, "admin", "correct-horse-battery")
	e.adminID = admin.ID
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["anton"].ID, true, rc)
	e.patrol.RequestRain(ctx, wed2230, rc)
	e.addPhotos(wedNight, photos)
	e.tick(afterWed)
	if n := e.night(wedNight); n.Status != "RAIN_REQUESTED" || !n.NeedsReview {
		t.Fatalf("setup: %+v", n)
	}
	return e
}

func TestConfirmRain(t *testing.T) {
	e := rainRequestedAndClosed(t, 3)
	ctx := context.Background()
	if err := e.nights.ConfirmRain(ctx, e.adminID, wedNight); err != nil {
		t.Fatal(err)
	}
	if n := e.night(wedNight); n.Status != "RAINED_OUT" || n.NeedsReview {
		t.Fatalf("%+v", n)
	}
	for who, v := range e.fines(wedNight) {
		if v != "RAINED_OUT/0" {
			t.Errorf("%s: %s", who, v)
		}
	}
	if err := e.nights.ConfirmRain(ctx, e.adminID, wedNight); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("confirming a night that is no longer requested: %v", err)
	}
}

func TestRejectRainFinesUseTheFrozenRate(t *testing.T) {
	e := rainRequestedAndClosed(t, 3)
	ctx := context.Background()
	set, _ := e.settings.Load(ctx)
	set.FinePerAbsence = 9000 // changed after the night closed at 5,000
	e.settings.Save(ctx, set)

	if err := e.nights.RejectRain(ctx, e.adminID, wedNight); err != nil {
		t.Fatal(err)
	}
	n := e.night(wedNight)
	if n.Status != "REPORTED" || n.NeedsReview {
		t.Fatalf("%+v", n)
	}
	want := map[string]string{"Anton (A-1)": "PRESENT/0", "Budi (B-2)": "ABSENT/5000", "C-3": "ABSENT/5000"}
	for who, v := range e.fines(wedNight) {
		if want[who] != v {
			t.Errorf("%s: %s, want %s", who, v, want[who])
		}
	}
	if err := e.nights.RejectRain(ctx, e.adminID, wedNight); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("rejecting twice: %v", err)
	}
}

func TestRejectRainKeepsNeedsReviewWhenPhotosAreMissing(t *testing.T) {
	e := rainRequestedAndClosed(t, 1)
	if err := e.nights.RejectRain(context.Background(), e.adminID, wedNight); err != nil {
		t.Fatal(err)
	}
	if n := e.night(wedNight); n.Status != "REPORTED" || !n.NeedsReview {
		t.Fatalf("%+v", n)
	}
}

func TestRejectRainBeforeCloseLeavesFinesToTheCloser(t *testing.T) {
	e := newCloseEnv(t, "2026-10-01")
	ctx := context.Background()
	ns := NewNightService(e.pool, e.settings, e.store)
	e.patrol.RequestRain(ctx, wed2230, rc)
	if err := ns.RejectRain(ctx, 1, wedNight); err != nil {
		t.Fatal(err)
	}
	if n := e.night(wedNight); n.Status != "REPORTED" || n.ClosedAt.Valid {
		t.Fatalf("%+v", n)
	}
	e.addPhotos(wedNight, 3)
	e.tick(afterWed)
	if got := e.fines(wedNight)["C-3"]; got != "ABSENT/5000" {
		t.Fatalf("closer fines: %s", got)
	}
}

func TestCorrectAttendance(t *testing.T) {
	e := newNightEnv(t)
	ctx := context.Background()
	set, _ := e.settings.Load(ctx)
	set.FinePerAbsence = 8000 // must not leak into the closed night
	e.settings.Save(ctx, set)
	budi, anton := e.members["budi"].ID, e.members["anton"].ID

	for name, reason := range map[string]string{"empty": "", "spaces": "     ", "short": "ok"} {
		err := e.nights.CorrectAttendance(ctx, e.adminID, wedNight, budi, domain.Present, reason)
		var ve ValidationErrors
		if !errors.As(err, &ve) || ve["reason"] == "" {
			t.Errorf("%s reason accepted: %v", name, err)
		}
	}
	if err := e.nights.CorrectAttendance(ctx, e.adminID, wedNight, budi, domain.RainedOut, "alasan cukup"); !errors.As(err, new(ValidationErrors)) {
		t.Fatalf("status RAINED_OUT: %v", err)
	}
	if got := e.fines(wedNight)["Budi (B-2)"]; got != "ABSENT/5000" {
		t.Fatalf("a rejected correction changed data: %s", got)
	}

	// Absent -> present removes the fine; present -> absent adds one at the frozen rate.
	if err := e.nights.CorrectAttendance(ctx, e.adminID, wedNight, budi, domain.Present, " Datang terlambat, dicatat pos "); err != nil {
		t.Fatal(err)
	}
	if err := e.nights.CorrectAttendance(ctx, e.adminID, wedNight, anton, domain.Absent, "Salah centang"); err != nil {
		t.Fatal(err)
	}
	got := e.fines(wedNight)
	if got["Budi (B-2)"] != "PRESENT/0" || got["Anton (A-1)"] != "ABSENT/5000" || got["C-3"] != "ABSENT/5000" {
		t.Fatalf("%v", got)
	}

	log, _ := db.New(e.pool).ListAuditByNight(ctx, e.night(wedNight).ID)
	var found bool
	for _, l := range log {
		if l.Action == "correct_attendance" && l.Reason != nil && *l.Reason == "Datang terlambat, dicatat pos" && l.Actor == "admin:1" {
			found = true
		}
	}
	if !found {
		t.Fatal("correction with its reason not in the audit log")
	}

	if err := e.nights.CorrectAttendance(ctx, e.adminID, wedNight, e.members["dewi"].ID, domain.Present, "bukan anggota tim"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("member not on that night: %v", err)
	}
	e.nights.MarkRain(ctx, e.adminID, wedNight)
	if err := e.nights.CorrectAttendance(ctx, e.adminID, wedNight, anton, domain.Present, "setelah hujan"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("correcting a rained-out night: %v", err)
	}
}

func TestCorrectionNeedsAClosedNight(t *testing.T) {
	e := newCloseEnv(t, "2026-10-01")
	ctx := context.Background()
	ns := NewNightService(e.pool, e.settings, e.store)
	e.patrol.Tonight(ctx, wed2230)
	err := ns.CorrectAttendance(ctx, 1, wedNight, e.members["anton"].ID, domain.Present, "dicatat pengurus")
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("correcting an open night: %v", err)
	}
	if got := e.fines(wedNight)["Anton (A-1)"]; got != "ABSENT/0" {
		t.Fatalf("an open night was changed: %s", got)
	}
}

func TestListAndDetail(t *testing.T) {
	e := rainRequestedAndClosed(t, 1)
	ctx := context.Background()

	// A second night, also closed but fine, in the same month.
	nextWed := wedNight.AddDays(7)
	e.tick(time.Date(2026, 10, 15, 6, 5, 0, 0, afterWed.Location()))

	pending, rows, err := e.nights.List(ctx, 2026, time.October, false)
	if err != nil || len(pending) != 1 || pending[0].Date != wedNight || pending[0].Photos != 1 || pending[0].Present != 1 || pending[0].Total != 3 {
		t.Fatalf("pending %+v err %v", pending, err)
	}
	if len(rows) != 1 || rows[0].Date != nextWed || rows[0].Status != domain.NightNoReport {
		t.Fatalf("rows %+v", rows)
	}
	_, review, _ := e.nights.List(ctx, 2026, time.October, true)
	if len(review) != 0 {
		t.Fatalf("review filter: %+v", review)
	}
	_, other, _ := e.nights.List(ctx, 2026, time.September, false)
	if len(other) != 0 {
		t.Fatalf("september: %+v", other)
	}

	d, err := e.nights.Detail(ctx, wedNight)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != domain.NightRainRequested || d.FirstReportedAt == nil || d.ClosedAt == nil || d.FineRate == nil || *d.FineRate != 5000 ||
		len(d.Members) != 3 || len(d.Photos) != 1 || d.MinPhotos != 3 || !d.NeedsReview {
		t.Fatalf("%+v", d)
	}
	var actions []string
	for _, a := range d.Audit {
		actions = append(actions, a.Action)
	}
	if got := strings.Join(actions, ","); got != "toggle_attendance,request_rain,close_night" {
		t.Fatalf("audit %s", got)
	}
	if _, err := e.nights.Detail(ctx, wedNight.AddDays(-30)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown night: %v", err)
	}
}

func TestFinesAndPayments(t *testing.T) {
	e := newNightEnv(t)
	ctx := context.Background()
	budi, c3 := e.members["budi"].ID, e.members["c3"].ID

	fm, err := e.fineSvc.Month(ctx, 2026, time.October)
	if err != nil || fm.TotalFines != 10000 || len(fm.Rows) != 2 || fm.Rows[0].DisplayName != "Budi (B-2)" || fm.Rows[0].Remaining != 5000 {
		t.Fatalf("%+v %v", fm, err)
	}

	if err := e.fineSvc.RecordPayment(ctx, e.adminID, PaymentInput{MemberID: budi, Year: 2026, Month: time.October, Amount: 3000, PaidAt: wedNight.AddDays(10)}); err != nil {
		t.Fatal(err)
	}
	if err := e.fineSvc.RecordPayment(ctx, e.adminID, PaymentInput{MemberID: budi, Year: 2026, Month: time.October, Amount: 2000, PaidAt: wedNight.AddDays(11)}); err != nil {
		t.Fatal(err)
	}
	if err := e.fineSvc.RecordPayment(ctx, e.adminID, PaymentInput{MemberID: c3, Year: 2026, Month: time.October, Amount: 6000, PaidAt: wedNight.AddDays(11)}); err != nil {
		t.Fatal(err)
	}
	if err := e.fineSvc.RecordPayment(ctx, e.adminID, PaymentInput{MemberID: c3, Year: 2026, Month: time.October, Amount: 0, PaidAt: wedNight}); !errors.As(err, new(ValidationErrors)) {
		t.Fatalf("zero amount: %v", err)
	}
	if err := e.fineSvc.RecordPayment(ctx, e.adminID, PaymentInput{MemberID: 9999, Year: 2026, Month: time.October, Amount: 10, PaidAt: wedNight}); !errors.As(err, new(ValidationErrors)) {
		t.Fatalf("unknown member: %v", err)
	}

	fm, _ = e.fineSvc.Month(ctx, 2026, time.October)
	byName := map[string]FineRow{}
	for _, r := range fm.Rows {
		byName[r.DisplayName] = r
	}
	if r := byName["Budi (B-2)"]; r.Total != 5000 || r.Paid != 5000 || r.Remaining != 0 {
		t.Fatalf("budi %+v", r)
	}
	if r := byName["C-3"]; r.Remaining != -1000 {
		t.Fatalf("overpaid c3 %+v", r)
	}
	if fm.TotalPaid != 11000 || len(fm.Payments) != 3 {
		t.Fatalf("%+v", fm)
	}
	if other, _ := e.fineSvc.Month(ctx, 2026, time.November); len(other.Rows) != 0 || len(other.Payments) != 0 {
		t.Fatalf("november: %+v", other)
	}

	if err := e.fineSvc.DeletePayment(ctx, e.adminID, fm.Payments[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := e.fineSvc.DeletePayment(ctx, e.adminID, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown payment: %v", err)
	}
	fm, _ = e.fineSvc.Month(ctx, 2026, time.October)
	if len(fm.Payments) != 2 {
		t.Fatalf("payments after delete: %d", len(fm.Payments))
	}
}
