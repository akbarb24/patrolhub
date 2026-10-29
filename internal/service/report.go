package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/storage"
)

// reportMaxAge limits which closed nights are still sent: older ones are dropped so the
// first deploy (or a gateway outage) never floods the group with old reports.
const reportMaxAge = 24 * time.Hour

// GroupMessenger sends a text to the RT's WhatsApp group (notify.GOWA in production).
type GroupMessenger interface {
	SendGroup(ctx context.Context, text string) error
}

// ReportService posts each closed night's report to the WhatsApp group, once.
type ReportService struct {
	pool     *pgxpool.Pool
	settings *SettingsService
	msg      GroupMessenger
	baseURL  string
}

func NewReportService(pool *pgxpool.Pool, settings *SettingsService, msg GroupMessenger, baseURL string) *ReportService {
	return &ReportService{pool: pool, settings: settings, msg: msg, baseURL: strings.TrimRight(baseURL, "/")}
}

// Tick sends every night closed in the last 24 hours that has not been sent yet. A night
// is marked sent only after the gateway accepted it, so a failure is retried on the next
// tick (and a crash in between sends it twice). Per-night errors are logged and joined.
func (s *ReportService) Tick(ctx context.Context, now time.Time) error {
	q := db.New(s.pool)
	nights, err := q.ListNightsToNotify(ctx, pgTime(now.Add(-reportMaxAge)))
	if err != nil || len(nights) == 0 {
		return err
	}
	set, err := s.settings.Load(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, n := range nights {
		if err := s.send(ctx, q, n, set.RTName, now); err != nil {
			d := domainDate(n.PatrolDate)
			slog.Error("whatsapp report", "night", d.String(), "err", err)
			errs = append(errs, fmt.Errorf("whatsapp report %s: %w", d, err))
		}
	}
	return errors.Join(errs...)
}

func (s *ReportService) send(ctx context.Context, q *db.Queries, n db.PatrolNight, rtName string, now time.Time) error {
	att, err := q.ListAttendanceByNight(ctx, n.ID)
	if err != nil {
		return err
	}
	if len(att) == 0 {
		return nil // no team, nothing to report (same rule as the recap); retried until it ages out
	}
	row := domain.NightRow{Date: domainDate(n.PatrolDate), Status: domain.NightStatus(n.Status), Note: n.SituationNote}
	for _, a := range att {
		row.Attendance = append(row.Attendance, domain.AttendanceRow{
			MemberID: a.MemberID, DisplayName: domain.DisplayName(a.MemberName, a.HouseBlock),
			Status: domain.AttendanceStatus(a.Status), Fine: int(a.FineAmount),
		})
	}

	photosURL := ""
	// rain reports carry no photo link, so they need no share token
	if row.Status != domain.NightRainRequested && row.Status != domain.NightRainedOut {
		photos, err := q.CountPhotosByNight(ctx, n.ID)
		if err != nil {
			return err
		}
		if photos > 0 {
			fresh := rand.Text()
			token, err := q.EnsureShareToken(ctx, db.EnsureShareTokenParams{ID: n.ID, ShareToken: &fresh})
			if err != nil {
				return err
			}
			photosURL = s.baseURL + "/shared/" + deref(token)
		}
	}

	if err := s.msg.SendGroup(ctx, domain.NightReportText(rtName, row, photosURL)); err != nil {
		return err
	}
	slog.Info("whatsapp report sent", "night", row.Date.String())
	return q.MarkWhatsAppSent(ctx, db.MarkWhatsAppSentParams{ID: n.ID, WaSentAt: pgTime(now)})
}

// validShareToken matches what rand.Text produces (26 base32 characters), so junk
// never reaches the database.
func validShareToken(t string) bool {
	if len(t) != 26 {
		return false
	}
	for _, c := range t {
		if !(c >= 'A' && c <= 'Z' || c >= '2' && c <= '7') {
			return false
		}
	}
	return true
}

// SharedPhotos returns the night behind a share link and its photo ids (oldest first).
func (s *NightService) SharedPhotos(ctx context.Context, token string) (domain.Date, []int64, error) {
	if !validShareToken(token) {
		return domain.Date{}, nil, ErrNotFound
	}
	q := db.New(s.pool)
	n, err := q.GetNightByShareToken(ctx, &token)
	if err != nil {
		return domain.Date{}, nil, notFound(err)
	}
	photos, err := q.ListPhotosByNight(ctx, n.ID)
	if err != nil {
		return domain.Date{}, nil, err
	}
	ids := make([]int64, len(photos))
	for i, p := range photos {
		ids[i] = p.ID
	}
	return domainDate(n.PatrolDate), ids, nil
}

// OpenSharedPhoto opens a photo through a share link; ErrNotFound unless the photo
// belongs to that link's night.
func (s *NightService) OpenSharedPhoto(ctx context.Context, token string, id int64) (io.ReadCloser, string, error) {
	if !validShareToken(token) {
		return nil, "", ErrNotFound
	}
	q := db.New(s.pool)
	n, err := q.GetNightByShareToken(ctx, &token)
	if err != nil {
		return nil, "", notFound(err)
	}
	p, err := q.GetPhoto(ctx, id)
	if err != nil {
		return nil, "", notFound(err)
	}
	if p.NightID != n.ID {
		return nil, "", ErrNotFound
	}
	f, err := s.store.Open(ctx, p.StorageKey)
	return f, storage.ContentType(p.StorageKey), err
}
