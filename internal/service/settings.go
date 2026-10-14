package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
)

// Keys of the setting table.
const (
	keyRTName         = "rt_name"
	keyWindowOpen     = "window_open"
	keyWindowClose    = "window_close"
	keyFinePerAbsence = "fine_per_absence"
	keyMinPhotos      = "min_photos"
	keyMaxPhotos      = "max_photos"
	keyGoLiveDate     = "go_live_date"
)

const maxPhotosLimit = 20

type Settings struct {
	RTName         string
	WindowOpen     domain.Clock
	WindowClose    domain.Clock
	FinePerAbsence int
	MinPhotos      int
	MaxPhotos      int
	GoLiveDate     *domain.Date // nil until the app goes live
}

// ValidationErrors maps a setting key (also the form field name) to a message
// in Indonesian, so a form can show it under the offending field.
type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + v[k]
	}
	return "invalid settings: " + strings.Join(parts, "; ")
}

// Validate returns ValidationErrors, or nil when the settings are acceptable.
func (s Settings) Validate() error {
	errs := ValidationErrors{}
	if strings.TrimSpace(s.RTName) == "" {
		errs[keyRTName] = "Nama RT wajib diisi."
	}
	if !validClock(s.WindowOpen) {
		errs[keyWindowOpen] = "Jam buka harus berformat HH:MM."
	}
	if !validClock(s.WindowClose) {
		errs[keyWindowClose] = "Jam tutup harus berformat HH:MM."
	}
	if errs[keyWindowOpen] == "" && errs[keyWindowClose] == "" {
		if s.WindowOpen.Hour*60+s.WindowOpen.Min <= s.WindowClose.Hour*60+s.WindowClose.Min {
			errs[keyWindowOpen] = "Jam buka harus lebih besar dari jam tutup (jendela melewati tengah malam)."
		}
	}
	if s.FinePerAbsence < 0 {
		errs[keyFinePerAbsence] = "Denda tidak boleh negatif."
	}
	if s.MinPhotos < 1 {
		errs[keyMinPhotos] = "Minimal foto paling sedikit 1."
	}
	if s.MaxPhotos > maxPhotosLimit {
		errs[keyMaxPhotos] = fmt.Sprintf("Maksimal foto tidak boleh lebih dari %d.", maxPhotosLimit)
	} else if s.MaxPhotos < s.MinPhotos {
		errs[keyMaxPhotos] = "Maksimal foto tidak boleh kurang dari minimal foto."
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func validClock(c domain.Clock) bool {
	return c.Hour >= 0 && c.Hour <= 23 && c.Min >= 0 && c.Min <= 59
}

// settingsFromMap decodes stored key/value rows. It reports unreadable values as
// a plain error: those mean the database is corrupt, not that a user mistyped.
func settingsFromMap(m map[string]string) (Settings, error) {
	get := func(key string) (string, error) {
		v, ok := m[key]
		if !ok {
			return "", fmt.Errorf("setting %q is missing", key)
		}
		return v, nil
	}
	var s Settings
	var err error
	str := func(key string, dst *string) {
		if err != nil {
			return
		}
		*dst, err = get(key)
	}
	clock := func(key string, dst *domain.Clock) {
		if err != nil {
			return
		}
		var v string
		if v, err = get(key); err == nil {
			*dst, err = domain.ParseClock(v)
		}
	}
	num := func(key string, dst *int) {
		if err != nil {
			return
		}
		var v string
		if v, err = get(key); err == nil {
			if *dst, err = strconv.Atoi(v); err != nil {
				err = fmt.Errorf("setting %q: %w", key, err)
			}
		}
	}
	str(keyRTName, &s.RTName)
	clock(keyWindowOpen, &s.WindowOpen)
	clock(keyWindowClose, &s.WindowClose)
	num(keyFinePerAbsence, &s.FinePerAbsence)
	num(keyMinPhotos, &s.MinPhotos)
	num(keyMaxPhotos, &s.MaxPhotos)
	if err != nil {
		return Settings{}, err
	}
	live, err := get(keyGoLiveDate)
	if err != nil {
		return Settings{}, err
	}
	if live != "" {
		d, err := domain.ParseDate(live)
		if err != nil {
			return Settings{}, fmt.Errorf("setting %q: %w", keyGoLiveDate, err)
		}
		s.GoLiveDate = &d
	}
	return s, nil
}

func (s Settings) toMap() map[string]string {
	live := ""
	if s.GoLiveDate != nil {
		live = s.GoLiveDate.String()
	}
	return map[string]string{
		keyRTName:         strings.TrimSpace(s.RTName),
		keyWindowOpen:     s.WindowOpen.String(),
		keyWindowClose:    s.WindowClose.String(),
		keyFinePerAbsence: strconv.Itoa(s.FinePerAbsence),
		keyMinPhotos:      strconv.Itoa(s.MinPhotos),
		keyMaxPhotos:      strconv.Itoa(s.MaxPhotos),
		keyGoLiveDate:     live,
	}
}

type SettingsService struct {
	pool *pgxpool.Pool
}

func NewSettingsService(pool *pgxpool.Pool) *SettingsService {
	return &SettingsService{pool: pool}
}

func (s *SettingsService) Load(ctx context.Context) (Settings, error) {
	rows, err := db.New(s.pool).ListSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	return settingsFromMap(m)
}

// Save validates and stores all settings atomically. Invalid input yields ValidationErrors.
func (s *SettingsService) Save(ctx context.Context, in Settings) error {
	if err := in.Validate(); err != nil {
		return err
	}
	return WithTx(ctx, s.pool, func(q *db.Queries) error {
		for k, v := range in.toMap() {
			if err := q.UpsertSetting(ctx, db.UpsertSettingParams{Key: k, Value: v}); err != nil {
				return err
			}
		}
		return nil
	})
}
