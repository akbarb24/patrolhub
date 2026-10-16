package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/storage"
)

var ErrInvalidState = errors.New("action not allowed in the night's current state")

const minReasonRunes = 5

// NightRow is one line of the admin night list.
type NightRow struct {
	Date        domain.Date
	Status      domain.NightStatus
	Present     int
	Total       int
	Photos      int
	NeedsReview bool
	Closed      bool
}

type DetailMember struct {
	MemberID    int64
	DisplayName string
	Status      domain.AttendanceStatus
	Source      domain.AttendanceSource
	Fine        int
}

type DetailPhoto struct {
	ID        int64
	TakenAt   time.Time
	Latitude  *float64
	Longitude *float64
}

type AuditEntry struct {
	At     time.Time
	Actor  string
	Action string
	Reason string
	IP     string
	Detail string
}

// NightDetail is everything the admin sees about one night.
type NightDetail struct {
	Date            domain.Date
	Status          domain.NightStatus
	Situation       string
	FirstReportedAt *time.Time
	RainRequestedAt *time.Time
	ClosedAt        *time.Time
	FineRate        *int
	NeedsReview     bool
	Members         []DetailMember
	TotalFine       int
	Photos          []DetailPhoto
	MinPhotos       int
	Audit           []AuditEntry
}

type NightService struct {
	pool     *pgxpool.Pool
	settings *SettingsService
	store    storage.PhotoStore
}

func NewNightService(pool *pgxpool.Pool, settings *SettingsService, store storage.PhotoStore) *NightService {
	return &NightService{pool: pool, settings: settings, store: store}
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func (s *NightService) row(ctx context.Context, q *db.Queries, n db.PatrolNight) (NightRow, error) {
	att, err := q.ListAttendanceByNight(ctx, n.ID)
	if err != nil {
		return NightRow{}, err
	}
	photos, err := q.CountPhotosByNight(ctx, n.ID)
	if err != nil {
		return NightRow{}, err
	}
	r := NightRow{
		Date: domainDate(n.PatrolDate), Status: domain.NightStatus(n.Status), Total: len(att),
		Photos: int(photos), NeedsReview: n.NeedsReview, Closed: n.ClosedAt.Valid,
	}
	for _, a := range att {
		if a.Status == string(domain.Present) {
			r.Present++
		}
	}
	return r, nil
}

// List returns the nights awaiting a rain decision (any month, newest first) and the
// nights of the given month (newest first), optionally only those needing review.
func (s *NightService) List(ctx context.Context, year int, month time.Month, onlyReview bool) (pending, rows []NightRow, err error) {
	q := db.New(s.pool)
	rain, err := q.ListNightsByStatus(ctx, string(domain.NightRainRequested))
	if err != nil {
		return nil, nil, err
	}
	isPending := map[int64]bool{}
	for _, n := range rain {
		isPending[n.ID] = true
		r, err := s.row(ctx, q, n)
		if err != nil {
			return nil, nil, err
		}
		pending = append(pending, r)
	}

	first := domain.Date{Year: year, Month: month, Day: 1}
	last := domain.Date{Year: year, Month: month + 1, Day: 1}.AddDays(-1)
	nights, err := q.ListNightsBetween(ctx, db.ListNightsBetweenParams{FromDate: pgDate(first), ToDate: pgDate(last)})
	if err != nil {
		return nil, nil, err
	}
	for i := len(nights) - 1; i >= 0; i-- {
		n := nights[i]
		if isPending[n.ID] || onlyReview && !n.NeedsReview {
			continue
		}
		r, err := s.row(ctx, q, n)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, r)
	}
	return pending, rows, nil
}

func (s *NightService) Detail(ctx context.Context, date domain.Date) (NightDetail, error) {
	q := db.New(s.pool)
	n, err := q.GetNightByDate(ctx, pgDate(date))
	if err != nil {
		return NightDetail{}, notFound(err)
	}
	set, err := s.settings.Load(ctx)
	if err != nil {
		return NightDetail{}, err
	}
	d := NightDetail{
		Date: date, Status: domain.NightStatus(n.Status), Situation: n.SituationNote, NeedsReview: n.NeedsReview, MinPhotos: set.MinPhotos,
		FirstReportedAt: timePtr(n.FirstReportedAt),
		RainRequestedAt: timePtr(n.RainRequestedAt),
		ClosedAt:        timePtr(n.ClosedAt),
	}
	if n.FineRate != nil {
		v := int(*n.FineRate)
		d.FineRate = &v
	}

	att, err := q.ListAttendanceByNight(ctx, n.ID)
	if err != nil {
		return d, err
	}
	for _, a := range att {
		d.Members = append(d.Members, DetailMember{
			MemberID: a.MemberID, DisplayName: domain.DisplayName(a.MemberName, a.HouseBlock),
			Status: domain.AttendanceStatus(a.Status), Source: domain.AttendanceSource(a.Source), Fine: int(a.FineAmount),
		})
		d.TotalFine += int(a.FineAmount)
	}
	sort.Slice(d.Members, func(i, j int) bool { return d.Members[i].DisplayName < d.Members[j].DisplayName })

	photos, err := q.ListPhotosByNight(ctx, n.ID)
	if err != nil {
		return d, err
	}
	for _, p := range photos {
		d.Photos = append(d.Photos, DetailPhoto{ID: p.ID, TakenAt: p.TakenAt.Time, Latitude: p.Latitude, Longitude: p.Longitude})
	}

	log, err := q.ListAuditByNight(ctx, n.ID)
	if err != nil {
		return d, err
	}
	for _, l := range log {
		d.Audit = append(d.Audit, AuditEntry{
			At: l.CreatedAt.Time, Actor: l.Actor, Action: l.Action, Reason: deref(l.Reason), IP: deref(l.Ip), Detail: compactDetail(l.Detail),
		})
	}
	return d, nil
}

// compactDetail renders audit JSON without the night_id every entry carries.
func compactDetail(raw []byte) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	delete(m, "night_id")
	if len(m) == 0 {
		return ""
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// adminAction runs fn on the locked night inside a transaction and writes the audit entry.
func (s *NightService) adminAction(ctx context.Context, adminID int64, date domain.Date, action, reason string,
	fn func(q *db.Queries, n db.PatrolNight, set Settings) (map[string]any, error)) error {
	set, err := s.settings.Load(ctx)
	if err != nil {
		return err
	}
	return WithTx(ctx, s.pool, func(q *db.Queries) error {
		n, err := q.GetNightByDateForUpdate(ctx, pgDate(date))
		if err != nil {
			return notFound(err)
		}
		detail, err := fn(q, n, set)
		if err != nil {
			return err
		}
		if detail == nil {
			detail = map[string]any{}
		}
		detail["night_id"] = n.ID
		raw, _ := json.Marshal(detail)
		return q.InsertAudit(ctx, db.InsertAuditParams{
			Entity: "patrol_night", EntityID: n.ID, Action: action, Actor: adminActor(adminID), Reason: optional(reason), Detail: raw,
		})
	})
}

func adminActor(id int64) string { return "admin:" + strconv.FormatInt(id, 10) }

// rainOut makes the whole night RAINED_OUT with no fines. Closed nights stay closed.
func rainOut(ctx context.Context, q *db.Queries, n db.PatrolNight) error {
	if err := q.SetNightStatus(ctx, db.SetNightStatusParams{ID: n.ID, Status: string(domain.NightRainedOut)}); err != nil {
		return err
	}
	rows, err := q.ListAttendanceByNight(ctx, n.ID)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := q.SetAttendanceStatus(ctx, db.SetAttendanceStatusParams{NightID: n.ID, MemberID: r.MemberID, Status: string(domain.RainedOut)}); err != nil {
			return err
		}
		if err := q.SetAttendanceFine(ctx, db.SetAttendanceFineParams{NightID: n.ID, MemberID: r.MemberID, FineAmount: 0}); err != nil {
			return err
		}
	}
	return q.SetNeedsReview(ctx, db.SetNeedsReviewParams{ID: n.ID, NeedsReview: false})
}

// MarkRain declares the night rained out, before or after it closed.
func (s *NightService) MarkRain(ctx context.Context, adminID int64, date domain.Date) error {
	return s.adminAction(ctx, adminID, date, "mark_rain", "", func(q *db.Queries, n db.PatrolNight, _ Settings) (map[string]any, error) {
		return map[string]any{"from": n.Status}, rainOut(ctx, q, n)
	})
}

// ConfirmRain accepts a reporter's rain request.
func (s *NightService) ConfirmRain(ctx context.Context, adminID int64, date domain.Date) error {
	return s.adminAction(ctx, adminID, date, "confirm_rain", "", func(q *db.Queries, n db.PatrolNight, _ Settings) (map[string]any, error) {
		if n.Status != string(domain.NightRainRequested) {
			return nil, ErrInvalidState
		}
		return nil, rainOut(ctx, q, n)
	})
}

// RejectRain turns a rain request down: the night counts as a normal reported night.
// If it already closed, fines are computed now from the rate frozen at closing.
func (s *NightService) RejectRain(ctx context.Context, adminID int64, date domain.Date) error {
	return s.adminAction(ctx, adminID, date, "reject_rain", "", func(q *db.Queries, n db.PatrolNight, set Settings) (map[string]any, error) {
		if n.Status != string(domain.NightRainRequested) {
			return nil, ErrInvalidState
		}
		if err := q.SetNightStatus(ctx, db.SetNightStatusParams{ID: n.ID, Status: string(domain.NightReported)}); err != nil {
			return nil, err
		}
		if !n.ClosedAt.Valid {
			return nil, nil // the closer will compute fines as for any reported night
		}
		rate := fineRate(n, set)
		rows, err := q.ListAttendanceByNight(ctx, n.ID)
		if err != nil {
			return nil, err
		}
		statuses := make(map[int64]domain.AttendanceStatus, len(rows))
		for _, r := range rows {
			statuses[r.MemberID] = domain.AttendanceStatus(r.Status)
		}
		for id, fine := range domain.RecalculateFines(statuses, rate) {
			if err := q.SetAttendanceFine(ctx, db.SetAttendanceFineParams{NightID: n.ID, MemberID: id, FineAmount: int32(fine)}); err != nil {
				return nil, err
			}
		}
		photos, err := q.CountPhotosByNight(ctx, n.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"fine_rate": rate}, q.SetNeedsReview(ctx, db.SetNeedsReviewParams{ID: n.ID, NeedsReview: int(photos) < set.MinPhotos})
	})
}

// fineRate is the rate frozen on the night, or today's rate for a night closed without one.
func fineRate(n db.PatrolNight, set Settings) int {
	if n.FineRate != nil {
		return int(*n.FineRate)
	}
	return set.FinePerAbsence
}

// CorrectAttendance changes one member's status on a closed night, with a mandatory
// reason, and recalculates that member's fine from the night's frozen rate.
func (s *NightService) CorrectAttendance(ctx context.Context, adminID int64, date domain.Date, memberID int64, status domain.AttendanceStatus, reason string) error {
	reason = strings.TrimSpace(reason)
	errs := ValidationErrors{}
	if utf8.RuneCountInString(reason) < minReasonRunes {
		errs["reason"] = "Alasan wajib diisi (minimal 5 karakter)."
	}
	if status != domain.Present && status != domain.Absent {
		errs["status"] = "Pilih Hadir atau Tidak hadir."
	}
	if len(errs) > 0 {
		return errs
	}
	return s.adminAction(ctx, adminID, date, "correct_attendance", reason, func(q *db.Queries, n db.PatrolNight, set Settings) (map[string]any, error) {
		// Only closed nights: on an open night the closer would overwrite the change
		// (a night nobody reported is closed as "no report", everyone absent).
		if !n.ClosedAt.Valid || n.Status == string(domain.NightRainedOut) {
			return nil, ErrInvalidState
		}
		rows, err := q.ListAttendanceByNight(ctx, n.ID)
		if err != nil {
			return nil, err
		}
		var old string
		found := false
		for _, r := range rows {
			if r.MemberID == memberID {
				old, found = r.Status, true
			}
		}
		if !found {
			return nil, ErrNotFound
		}
		if err := q.SetAttendanceStatus(ctx, db.SetAttendanceStatusParams{NightID: n.ID, MemberID: memberID, Status: string(status)}); err != nil {
			return nil, err
		}
		fine := domain.RecalculateFines(map[int64]domain.AttendanceStatus{memberID: status}, fineRate(n, set))[memberID]
		if err := q.SetAttendanceFine(ctx, db.SetAttendanceFineParams{NightID: n.ID, MemberID: memberID, FineAmount: int32(fine)}); err != nil {
			return nil, err
		}
		return map[string]any{"member_id": memberID, "from": old, "to": status, "fine": fine}, nil
	})
}

// OpenPhoto opens any stored photo for an admin.
func (s *NightService) OpenPhoto(ctx context.Context, id int64) (io.ReadCloser, string, error) {
	p, err := db.New(s.pool).GetPhoto(ctx, id)
	if err != nil {
		return nil, "", notFound(err)
	}
	f, err := s.store.Open(ctx, p.StorageKey)
	return f, storage.ContentType(p.StorageKey), err
}
