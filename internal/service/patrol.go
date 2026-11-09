package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/storage"
)

var (
	ErrWindowClosed  = errors.New("report window is closed")
	ErrNotInTeam     = errors.New("member is not in tonight's team")
	ErrAlreadyInTeam = errors.New("member is already in tonight's team")
	ErrTooLong       = errors.New("situation note is too long")
	ErrTooManyPhotos = errors.New("photo limit reached")
	ErrPhotoTooLarge = errors.New("photo is too large")
	ErrBadImageType  = errors.New("unsupported image type")
	ErrNoSituation   = errors.New("situation note is empty")
	ErrTooFewPhotos  = errors.New("not enough photos to submit")
)

const (
	maxSituationRunes = 1000
	MaxPhotoBytes     = 1 << 20
)

// ReportContext identifies the (anonymous) reporter for the audit log.
type ReportContext struct {
	IP, UserAgent string
}

type TeamRow struct {
	MemberID    int64
	DisplayName string
	Present     bool
	Source      domain.AttendanceSource
}

type PhotoItem struct {
	ID      int64
	TakenAt time.Time
}

type Candidate struct {
	ID    int64
	Label string
}

// TonightView is everything the report page shows.
type TonightView struct {
	Open   bool // inside the report window
	Locked bool // open window, but nothing can be edited (submitted, rain requested/out, or already closed)
	// Submitted: the report was sent tonight; with RainRequested it is the reason for
	// Locked while the night is still open, and the page shows a read-only summary.
	Submitted   bool
	SubmittedAt time.Time
	NightDate   domain.Date
	OpensAt     time.Time
	// NextOpensAt is when reporting is next possible while the page is closed or
	// locked; UntilOpen is the time left from now (drives the countdown).
	NextOpensAt time.Time
	UntilOpen   time.Duration
	// UntilClose is the time left in an open, unlocked window (drives the reload into the lock modal).
	UntilClose time.Duration
	RTName     string
	WindowOpen domain.Clock

	Team          []TeamRow
	Situation     string
	Photos        []PhotoItem
	MinPhotos     int
	MaxPhotos     int
	RainRequested bool
	Candidates    []Candidate // active members not on tonight's team (walk-in)
}

type PatrolService struct {
	pool     *pgxpool.Pool
	settings *SettingsService
	store    storage.PhotoStore
}

func NewPatrolService(pool *pgxpool.Pool, settings *SettingsService, store storage.PhotoStore) *PatrolService {
	return &PatrolService{pool: pool, settings: settings, store: store}
}

// openNight makes sure the night's row exists and locks it for the rest of the transaction.
func openNight(ctx context.Context, q *db.Queries, date domain.Date) (db.PatrolNight, error) {
	if err := q.InsertNightIfMissing(ctx, pgDate(date)); err != nil {
		return db.PatrolNight{}, err
	}
	return q.GetNightByDateForUpdate(ctx, pgDate(date))
}

// nightClosed: the night is over for reporters (closed by the job or rained out).
func nightClosed(n db.PatrolNight) bool {
	return n.ClosedAt.Valid || n.Status == string(domain.NightRainedOut)
}

// nightLocked: reporters can no longer change anything: the night is closed, the
// report was submitted, or rain was requested.
func nightLocked(n db.PatrolNight) bool {
	return nightClosed(n) || n.SubmittedAt.Valid || n.Status == string(domain.NightRainRequested)
}

// Summary reports whether the open-window page should show the read-only summary
// (submitted or rain requested) instead of the closed lock modal.
func (v TonightView) Summary() bool {
	return v.Open && v.Locked && (v.Submitted || v.RainRequested)
}

// Tonight builds the report page. Inside the window it also creates the night and
// syncs the team into attendance rows; outside it only previews the coming team.
func (s *PatrolService) Tonight(ctx context.Context, now time.Time) (TonightView, error) {
	set, err := s.settings.Load(ctx)
	if err != nil {
		return TonightView{}, err
	}
	w := domain.CurrentWindow(now, set.WindowOpen, set.WindowClose)
	v := TonightView{
		Open: w.Open, NightDate: w.NightDate, OpensAt: w.OpensAt,
		RTName: set.RTName, WindowOpen: set.WindowOpen, MinPhotos: set.MinPhotos, MaxPhotos: set.MaxPhotos,
	}

	if !w.Open {
		v.NextOpensAt, v.UntilOpen = w.OpensAt, w.OpensAt.Sub(now)
		entries, err := teamEntries(ctx, db.New(s.pool), w.NightDate)
		if err != nil {
			return v, err
		}
		for _, e := range entries {
			v.Team = append(v.Team, TeamRow{MemberID: e.MemberID, DisplayName: e.DisplayName, Source: e.Source})
		}
		return v, nil
	}

	err = WithTx(ctx, s.pool, func(q *db.Queries) error {
		night, err := openNight(ctx, q, w.NightDate)
		if err != nil {
			return err
		}
		v.Locked = nightLocked(night)
		v.Submitted = night.SubmittedAt.Valid
		if v.Submitted {
			v.SubmittedAt = night.SubmittedAt.Time
		}
		if nightClosed(night) {
			v.NextOpensAt = w.NightDate.AddDays(1).At(set.WindowOpen.Hour, set.WindowOpen.Min)
			v.UntilOpen = v.NextOpensAt.Sub(now)
		} else {
			v.UntilClose = w.ClosesAt.Sub(now)
		}
		if !v.Locked {
			if err := syncTeam(ctx, q, night); err != nil {
				return err
			}
		}
		v.Situation = night.SituationNote
		v.RainRequested = night.Status == string(domain.NightRainRequested)

		rows, err := attendanceRows(ctx, q, night.ID)
		if err != nil {
			return err
		}
		v.Team = rows

		photos, err := q.ListPhotosByNight(ctx, night.ID)
		if err != nil {
			return err
		}
		for _, p := range photos {
			v.Photos = append(v.Photos, PhotoItem{ID: p.ID, TakenAt: p.TakenAt.Time})
		}

		if !v.Locked {
			members, err := q.ListActiveMembers(ctx)
			if err != nil {
				return err
			}
			onTeam := map[int64]bool{}
			for _, r := range rows {
				onTeam[r.MemberID] = true
			}
			for _, m := range members {
				if !onTeam[m.ID] {
					v.Candidates = append(v.Candidates, Candidate{ID: m.ID, Label: MemberLabel(m)})
				}
			}
		}
		return nil
	})
	return v, err
}

func attendanceRows(ctx context.Context, q *db.Queries, nightID int64) ([]TeamRow, error) {
	list, err := q.ListAttendanceByNight(ctx, nightID)
	if err != nil {
		return nil, err
	}
	rows := make([]TeamRow, len(list))
	for i, a := range list {
		rows[i] = TeamRow{
			MemberID:    a.MemberID,
			DisplayName: domain.DisplayName(a.MemberName, a.HouseBlock),
			Present:     a.Status == string(domain.Present),
			Source:      domain.AttendanceSource(a.Source),
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].DisplayName < rows[j].DisplayName })
	return rows, nil
}

type audit struct {
	action string
	entity string // defaults to patrol_night
	id     int64  // defaults to the night's id
	detail map[string]any
}

type writeCtx struct {
	q     *db.Queries
	night db.PatrolNight
	set   Settings
	now   time.Time
}

// write runs one reporter action inside the open window: it locks the night, syncs
// the team, runs fn and writes the audit entry, all in one transaction.
func (s *PatrolService) write(ctx context.Context, now time.Time, rc ReportContext, fn func(w writeCtx) (audit, error)) error {
	set, err := s.settings.Load(ctx)
	if err != nil {
		return err
	}
	win := domain.CurrentWindow(now, set.WindowOpen, set.WindowClose)
	if !win.Open {
		return ErrWindowClosed
	}
	return WithTx(ctx, s.pool, func(q *db.Queries) error {
		night, err := openNight(ctx, q, win.NightDate)
		if err != nil {
			return err
		}
		if nightLocked(night) {
			return ErrNightClosed
		}
		if err := syncTeam(ctx, q, night); err != nil {
			return err
		}
		a, err := fn(writeCtx{q: q, night: night, set: set, now: now})
		if err != nil {
			return err
		}
		if a.entity == "" {
			a.entity, a.id = "patrol_night", night.ID
		}
		if a.detail == nil {
			a.detail = map[string]any{}
		}
		a.detail["night_id"] = night.ID // lets the admin night history find child entities
		detail, _ := json.Marshal(a.detail)
		return q.InsertAudit(ctx, db.InsertAuditParams{
			Entity: a.entity, EntityID: a.id, Action: a.action, Actor: "ANONYMOUS",
			Ip: optional(rc.IP), UserAgent: optional(truncate(rc.UserAgent, 300)), Detail: detail,
		})
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// SubmitInput is the report form: the note, the team members ticked present, and
// members added as walk-ins (they count as present).
type SubmitInput struct {
	Note    string
	Present []int64
	WalkIns []int64
}

// Submit saves the whole report at once and locks it: attendance for the team
// (unticked = absent), walk-ins and the situation note. It needs a note and at least
// min_photos photos. After it the report cannot be changed by reporters.
func (s *PatrolService) Submit(ctx context.Context, now time.Time, in SubmitInput, rc ReportContext) error {
	note := trimSpace(in.Note)
	if utf8.RuneCountInString(note) > maxSituationRunes {
		return ErrTooLong
	}
	if note == "" {
		return ErrNoSituation
	}
	return s.write(ctx, now, rc, func(w writeCtx) (audit, error) {
		photos, err := w.q.CountPhotosByNight(ctx, w.night.ID)
		if err != nil {
			return audit{}, err
		}
		if int(photos) < w.set.MinPhotos {
			return audit{}, ErrTooFewPhotos
		}
		rows, err := attendanceRows(ctx, w.q, w.night.ID)
		if err != nil {
			return audit{}, err
		}
		onTeam := make(map[int64]bool, len(rows))
		for _, r := range rows {
			onTeam[r.MemberID] = true
		}

		walkIns := uniq(in.WalkIns)
		for _, id := range walkIns {
			if onTeam[id] {
				return audit{}, ErrAlreadyInTeam
			}
			m, err := w.q.GetMember(ctx, id)
			if err != nil || !m.Active {
				return audit{}, ErrNotInTeam
			}
			err = w.q.UpsertAttendance(ctx, db.UpsertAttendanceParams{
				NightID: w.night.ID, MemberID: id, Status: string(domain.Present), Source: string(domain.SourceWalkIn),
			})
			if err != nil {
				return audit{}, err
			}
		}

		present := uniq(in.Present)
		presentSet := make(map[int64]bool, len(present))
		for _, id := range present {
			if !onTeam[id] && !slices.Contains(walkIns, id) {
				return audit{}, ErrNotInTeam
			}
			presentSet[id] = true
		}
		for _, r := range rows {
			status := domain.Absent
			if presentSet[r.MemberID] {
				status = domain.Present
			}
			if err := w.q.SetAttendanceStatus(ctx, db.SetAttendanceStatusParams{NightID: w.night.ID, MemberID: r.MemberID, Status: string(status)}); err != nil {
				return audit{}, err
			}
		}

		if err := w.q.UpdateSituation(ctx, db.UpdateSituationParams{ID: w.night.ID, SituationNote: note}); err != nil {
			return audit{}, err
		}
		if err := w.q.SubmitNight(ctx, db.SubmitNightParams{ID: w.night.ID, SubmittedAt: pgTime(now)}); err != nil {
			return audit{}, err
		}
		return audit{action: "submit_report", detail: map[string]any{
			"present": present, "walk_ins": walkIns, "note_length": utf8.RuneCountInString(note), "photos": photos,
		}}, nil
	})
}

func uniq(ids []int64) []int64 {
	out := []int64{}
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func trimSpace(s string) string { return string(bytes.TrimSpace([]byte(s))) }

// AddPhoto stores one photo (JPEG or WebP, at most 1 MB) for tonight.
func (s *PatrolService) AddPhoto(ctx context.Context, now time.Time, file io.Reader, size int64, lat, lng *float64, rc ReportContext) (PhotoItem, error) {
	if size > MaxPhotoBytes {
		return PhotoItem{}, ErrPhotoTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxPhotoBytes+1))
	if err != nil {
		return PhotoItem{}, err
	}
	if len(data) > MaxPhotoBytes {
		return PhotoItem{}, ErrPhotoTooLarge
	}
	var ext string
	switch http.DetectContentType(data[:min(len(data), 512)]) {
	case "image/jpeg":
		ext = "jpg"
	case "image/webp":
		ext = "webp"
	default:
		return PhotoItem{}, ErrBadImageType
	}
	if lat != nil && (*lat < -90 || *lat > 90) || lng != nil && (*lng < -180 || *lng > 180) {
		lat, lng = nil, nil // a bogus location must not block the report
	}

	var item PhotoItem
	var savedKey string
	err = s.write(ctx, now, rc, func(w writeCtx) (audit, error) {
		count, err := w.q.CountPhotosByNight(ctx, w.night.ID)
		if err != nil {
			return audit{}, err
		}
		if int(count) >= w.set.MaxPhotos {
			return audit{}, ErrTooManyPhotos
		}
		key := fmt.Sprintf("%04d/%02d/%02d/%s.%s", w.night.PatrolDate.Time.Year(), int(w.night.PatrolDate.Time.Month()), w.night.PatrolDate.Time.Day(), uuid.NewString(), ext)
		if err := s.store.Save(ctx, key, bytes.NewReader(data)); err != nil {
			return audit{}, err
		}
		savedKey = key
		if err := w.q.MarkReported(ctx, db.MarkReportedParams{ID: w.night.ID, FirstReportedAt: pgTime(now)}); err != nil {
			return audit{}, err
		}
		p, err := w.q.InsertPhoto(ctx, db.InsertPhotoParams{
			NightID: w.night.ID, StorageKey: key, TakenAt: pgTime(now), Latitude: lat, Longitude: lng, SizeBytes: int32(len(data)),
		})
		if err != nil {
			return audit{}, err
		}
		item = PhotoItem{ID: p.ID, TakenAt: now}
		return audit{action: "upload_photo", entity: "patrol_photo", id: p.ID, detail: map[string]any{"size_bytes": len(data), "has_location": lat != nil && lng != nil}}, nil
	})
	if err != nil && savedKey != "" {
		s.store.Delete(ctx, savedKey) // the transaction rolled back; do not leave an orphan file
	}
	return item, err
}

// RequestRain asks the admins to confirm a rained-out night. It cannot be withdrawn
// and locks the report (a second request fails with ErrNightClosed).
func (s *PatrolService) RequestRain(ctx context.Context, now time.Time, rc ReportContext) error {
	return s.write(ctx, now, rc, func(w writeCtx) (audit, error) {
		err := w.q.SetNightStatus(ctx, db.SetNightStatusParams{ID: w.night.ID, Status: string(domain.NightRainRequested), RainRequestedAt: pgTime(now)})
		if err != nil {
			return audit{}, err
		}
		if err := w.q.MarkReported(ctx, db.MarkReportedParams{ID: w.night.ID, FirstReportedAt: pgTime(now)}); err != nil {
			return audit{}, err
		}
		return audit{action: "request_rain"}, nil
	})
}

// OpenTonightPhoto opens a photo for the reporter, but only while that photo's night
// is the one currently open. Afterwards photos are for admins only.
func (s *PatrolService) OpenTonightPhoto(ctx context.Context, now time.Time, id int64) (io.ReadCloser, string, error) {
	set, err := s.settings.Load(ctx)
	if err != nil {
		return nil, "", err
	}
	w := domain.CurrentWindow(now, set.WindowOpen, set.WindowClose)
	if !w.Open {
		return nil, "", ErrNotFound
	}
	q := db.New(s.pool)
	night, err := q.GetNightByDate(ctx, pgDate(w.NightDate))
	if err != nil {
		return nil, "", notFound(err)
	}
	p, err := q.GetPhoto(ctx, id)
	if err != nil {
		return nil, "", notFound(err)
	}
	if p.NightID != night.ID || night.ClosedAt.Valid {
		return nil, "", ErrNotFound
	}
	f, err := s.store.Open(ctx, p.StorageKey)
	return f, storage.ContentType(p.StorageKey), err
}
