package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
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
	Open      bool // inside the report window
	Locked    bool // open window, but nothing can be edited (rain-out or already closed)
	NightDate domain.Date
	OpensAt   time.Time
	// NextOpensAt is when reporting is next possible while the page is closed or
	// locked; UntilOpen is the time left from now (drives the countdown).
	NextOpensAt time.Time
	UntilOpen   time.Duration
	RTName      string
	WindowOpen  domain.Clock

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

func nightLocked(n db.PatrolNight) bool {
	return n.ClosedAt.Valid || n.Status == string(domain.NightRainedOut)
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
		if v.Locked {
			v.NextOpensAt = w.NightDate.AddDays(1).At(set.WindowOpen.Hour, set.WindowOpen.Min)
			v.UntilOpen = v.NextOpensAt.Sub(now)
		} else {
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
// the team, runs fn, records the first report time and writes the audit entry,
// all in one transaction.
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
		if err := q.MarkReported(ctx, db.MarkReportedParams{ID: night.ID, FirstReportedAt: pgTime(now)}); err != nil {
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

// ToggleAttendance marks a team member present or absent and returns the updated row.
func (s *PatrolService) ToggleAttendance(ctx context.Context, now time.Time, memberID int64, present bool, rc ReportContext) (TeamRow, error) {
	var row TeamRow
	err := s.write(ctx, now, rc, func(w writeCtx) (audit, error) {
		rows, err := attendanceRows(ctx, w.q, w.night.ID)
		if err != nil {
			return audit{}, err
		}
		found := false
		for _, r := range rows {
			if r.MemberID == memberID {
				row, found = r, true
			}
		}
		if !found {
			return audit{}, ErrNotInTeam
		}
		status := domain.Absent
		if present {
			status = domain.Present
		}
		if err := w.q.SetAttendanceStatus(ctx, db.SetAttendanceStatusParams{NightID: w.night.ID, MemberID: memberID, Status: string(status)}); err != nil {
			return audit{}, err
		}
		row.Present = present
		return audit{action: "toggle_attendance", detail: map[string]any{"member_id": memberID, "present": present}}, nil
	})
	return row, err
}

// UpdateSituation replaces the situation note (at most 1,000 characters).
func (s *PatrolService) UpdateSituation(ctx context.Context, now time.Time, note string, rc ReportContext) error {
	note = trimSpace(note)
	if utf8.RuneCountInString(note) > maxSituationRunes {
		return ErrTooLong
	}
	return s.write(ctx, now, rc, func(w writeCtx) (audit, error) {
		if err := w.q.UpdateSituation(ctx, db.UpdateSituationParams{ID: w.night.ID, SituationNote: note}); err != nil {
			return audit{}, err
		}
		return audit{action: "update_situation", detail: map[string]any{"length": utf8.RuneCountInString(note)}}, nil
	})
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

// RequestRain asks the admins to confirm a rained-out night. It cannot be withdrawn.
func (s *PatrolService) RequestRain(ctx context.Context, now time.Time, rc ReportContext) error {
	return s.write(ctx, now, rc, func(w writeCtx) (audit, error) {
		if w.night.Status != string(domain.NightRainRequested) {
			err := w.q.SetNightStatus(ctx, db.SetNightStatusParams{ID: w.night.ID, Status: string(domain.NightRainRequested), RainRequestedAt: pgTime(now)})
			if err != nil {
				return audit{}, err
			}
		}
		return audit{action: "request_rain"}, nil
	})
}

// AddWalkIn adds an active member who joined outside the schedule; they count as present.
func (s *PatrolService) AddWalkIn(ctx context.Context, now time.Time, memberID int64, rc ReportContext) error {
	return s.write(ctx, now, rc, func(w writeCtx) (audit, error) {
		m, err := w.q.GetMember(ctx, memberID)
		if err != nil || !m.Active {
			return audit{}, ErrNotInTeam
		}
		rows, err := attendanceRows(ctx, w.q, w.night.ID)
		if err != nil {
			return audit{}, err
		}
		for _, r := range rows {
			if r.MemberID == memberID {
				return audit{}, ErrAlreadyInTeam
			}
		}
		err = w.q.UpsertAttendance(ctx, db.UpsertAttendanceParams{
			NightID: w.night.ID, MemberID: memberID, Status: string(domain.Present), Source: string(domain.SourceWalkIn),
		})
		if err != nil {
			return audit{}, err
		}
		return audit{action: "add_walk_in", detail: map[string]any{"member_id": memberID}}, nil
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
	return f, PhotoContentType(p.StorageKey), err
}

// PhotoContentType derives the media type from the storage key; AddPhoto only ever
// stores .jpg or .webp, chosen from the sniffed content.
func PhotoContentType(key string) string {
	if strings.HasSuffix(key, ".webp") {
		return "image/webp"
	}
	return "image/jpeg"
}
