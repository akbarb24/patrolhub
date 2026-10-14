package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/storage"
	"github.com/akbarbinaji/patrolhub/internal/testdb"
)

type patrolEnv struct {
	t        *testing.T
	pool     *pgxpool.Pool
	settings *SettingsService
	patrol   *PatrolService
	store    storage.LocalStore
	members  map[string]db.Member
}

// newPatrolEnv sets up Anton, Budi and a block-only member on the Wednesday team.
func newPatrolEnv(t *testing.T) *patrolEnv {
	t.Helper()
	pool := testdb.New(t)
	e := &patrolEnv{t: t, pool: pool, settings: NewSettingsService(pool), store: storage.LocalStore{Dir: t.TempDir()}, members: map[string]db.Member{}}
	e.patrol = NewPatrolService(pool, e.settings, e.store)

	ms, ss := NewMemberService(pool), NewScheduleService(pool)
	ctx := context.Background()
	for _, p := range []struct{ key, block, name string }{
		{"anton", "A-1", "Anton"}, {"budi", "B-2", "Budi"}, {"c3", "C-3", ""}, {"dewi", "D-4", "Dewi"},
	} {
		e.members[p.key] = mustMember(t, ms, p.block, p.name)
	}
	for _, k := range []string{"anton", "budi", "c3"} {
		if err := ss.AddRule(ctx, e.members[k].ID, 3, domain.Date{Year: 2026, Month: 1, Day: 1}); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

var rc = ReportContext{IP: "203.0.113.9", UserAgent: "TestBrowser/1.0"}

func jpeg(n int) []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, bytes.Repeat([]byte{1}, n)...)
}

func (e *patrolEnv) night(date domain.Date) db.PatrolNight {
	e.t.Helper()
	n, err := db.New(e.pool).GetNightByDate(context.Background(), pgDate(date))
	if err != nil {
		e.t.Fatalf("night %v: %v", date, err)
	}
	return n
}

func rowNames(rows []TeamRow) string {
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.DisplayName
	}
	return strings.Join(names, ",")
}

// Wednesday 2026-10-07 is the patrol night; 22:30 is inside the window.
var (
	wed2230  = time.Date(2026, 10, 7, 22, 30, 0, 0, testNow.Location())
	thu0030  = time.Date(2026, 10, 8, 0, 30, 0, 0, testNow.Location())
	wed2100  = time.Date(2026, 10, 7, 21, 0, 0, 0, testNow.Location())
	wedNight = domain.Date{Year: 2026, Month: 10, Day: 7}
)

func TestTonightOutsideWindowShowsComingTeamOnly(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	v, err := e.patrol.Tonight(ctx, wed2100)
	if err != nil {
		t.Fatal(err)
	}
	if v.Open || v.NightDate != wedNight || v.WindowOpen != (domain.Clock{Hour: 22}) || rowNames(v.Team) != "Anton (A-1),Budi (B-2),C-3" {
		t.Fatalf("%+v", v)
	}
	if _, err := db.New(e.pool).GetNightByDate(ctx, pgDate(wedNight)); err == nil {
		t.Fatal("viewing outside the window must not create the night")
	}
	if _, err := e.patrol.ToggleAttendance(ctx, wed2100, e.members["anton"].ID, true, rc); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("toggle outside window: %v", err)
	}
	if err := e.patrol.UpdateSituation(ctx, wed2100, "x", rc); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("situation outside window: %v", err)
	}
}

func TestTonightSyncsTeamAndToggleSurvivesRefresh(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	v, err := e.patrol.Tonight(ctx, wed2230)
	if err != nil || !v.Open || v.Locked || len(v.Team) != 3 {
		t.Fatalf("%+v %v", v, err)
	}
	for _, r := range v.Team {
		if r.Present {
			t.Errorf("%s starts present", r.DisplayName)
		}
	}
	if len(v.Candidates) != 1 || v.Candidates[0].Label != "Dewi (D-4)" || v.MinPhotos != 3 || v.MaxPhotos != 10 {
		t.Fatalf("candidates %+v min=%d max=%d", v.Candidates, v.MinPhotos, v.MaxPhotos)
	}

	row, err := e.patrol.ToggleAttendance(ctx, wed2230, e.members["anton"].ID, true, rc)
	if err != nil || !row.Present || row.DisplayName != "Anton (A-1)" {
		t.Fatalf("%+v %v", row, err)
	}
	v, _ = e.patrol.Tonight(ctx, wed2230.Add(time.Hour))
	present := 0
	for _, r := range v.Team {
		if r.Present {
			present++
			if r.MemberID != e.members["anton"].ID {
				t.Errorf("wrong member present: %s", r.DisplayName)
			}
		}
	}
	if present != 1 {
		t.Fatalf("present=%d", present)
	}
	if n := e.night(wedNight); !n.FirstReportedAt.Valid || !n.FirstReportedAt.Time.Equal(wed2230) {
		t.Fatalf("first_reported_at %+v", n.FirstReportedAt)
	}

	if _, err := e.patrol.ToggleAttendance(ctx, wed2230, e.members["dewi"].ID, true, rc); !errors.Is(err, ErrNotInTeam) {
		t.Fatalf("toggle non-member: %v", err)
	}
	if _, err := e.patrol.ToggleAttendance(ctx, wed2230, 99999, true, rc); !errors.Is(err, ErrNotInTeam) {
		t.Fatalf("toggle unknown: %v", err)
	}
}

func TestReportAfterMidnightBelongsToPreviousNight(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	if _, err := e.patrol.ToggleAttendance(ctx, thu0030, e.members["budi"].ID, true, rc); err != nil {
		t.Fatal(err)
	}
	n := e.night(wedNight)
	if !n.FirstReportedAt.Valid {
		t.Fatal("report at 00:30 must count for the previous night")
	}
	if _, err := db.New(e.pool).GetNightByDate(ctx, pgDate(wedNight.AddDays(1))); err == nil {
		t.Fatal("a Thursday night must not exist")
	}
}

func TestSyncDropsAbsentRemovedMembersButKeepsPresentOnes(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	ss := NewScheduleService(e.pool)
	e.patrol.Tonight(ctx, wed2230)
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["anton"].ID, true, rc)

	// An admin removes both Anton (present) and Budi (absent) for tonight.
	for _, k := range []string{"anton", "budi"} {
		if err := ss.CreateOverride(ctx, OverrideInput{Date: wedNight, Action: "REMOVE", MemberID: e.members[k].ID}); err != nil {
			t.Fatal(err)
		}
	}
	// ...and adds Dewi.
	if err := ss.CreateOverride(ctx, OverrideInput{Date: wedNight, Action: "ADD", MemberID: e.members["dewi"].ID}); err != nil {
		t.Fatal(err)
	}
	v, _ := e.patrol.Tonight(ctx, wed2230.Add(time.Minute))
	if rowNames(v.Team) != "Anton (A-1),C-3,Dewi (D-4)" {
		t.Fatalf("team %s", rowNames(v.Team))
	}
}

func TestSituation(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	if err := e.patrol.UpdateSituation(ctx, wed2230, "  Aman, lampu pos mati.  ", rc); err != nil {
		t.Fatal(err)
	}
	if got := e.night(wedNight).SituationNote; got != "Aman, lampu pos mati." {
		t.Fatalf("stored %q", got)
	}
	if err := e.patrol.UpdateSituation(ctx, wed2230, strings.Repeat("é", 1000), rc); err != nil {
		t.Fatalf("exactly 1000 characters must be accepted: %v", err)
	}
	if err := e.patrol.UpdateSituation(ctx, wed2230, strings.Repeat("é", 1001), rc); !errors.Is(err, ErrTooLong) {
		t.Fatalf("1001 characters: %v", err)
	}
	v, _ := e.patrol.Tonight(ctx, wed2230)
	if len([]rune(v.Situation)) != 1000 {
		t.Fatalf("a rejected note must not overwrite the stored one")
	}
}

func TestPhotos(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	lat, lng := -6.2, 106.8

	item, err := e.patrol.AddPhoto(ctx, wed2230, bytes.NewReader(jpeg(100)), 111, &lat, &lng, rc)
	if err != nil || item.ID == 0 {
		t.Fatalf("%+v %v", item, err)
	}
	photos, _ := db.New(e.pool).ListPhotosByNight(ctx, e.night(wedNight).ID)
	if len(photos) != 1 || !strings.HasPrefix(photos[0].StorageKey, "2026/10/07/") || !strings.HasSuffix(photos[0].StorageKey, ".jpg") ||
		photos[0].Latitude == nil || *photos[0].Latitude != lat {
		t.Fatalf("%+v", photos)
	}
	if _, err := os.Stat(e.store.Dir + "/" + photos[0].StorageKey); err != nil {
		t.Fatalf("file missing: %v", err)
	}

	// WebP is accepted, other types and oversized files are not.
	webp := append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), bytes.Repeat([]byte{0}, 50)...)
	if _, err := e.patrol.AddPhoto(ctx, wed2230, bytes.NewReader(webp), int64(len(webp)), nil, nil, rc); err != nil {
		t.Fatalf("webp: %v", err)
	}
	if _, err := e.patrol.AddPhoto(ctx, wed2230, strings.NewReader("<html>not a photo</html>"), 24, nil, nil, rc); !errors.Is(err, ErrBadImageType) {
		t.Fatalf("html: %v", err)
	}
	if _, err := e.patrol.AddPhoto(ctx, wed2230, bytes.NewReader(jpeg(MaxPhotoBytes)), MaxPhotoBytes+11, nil, nil, rc); !errors.Is(err, ErrPhotoTooLarge) {
		t.Fatalf("declared size: %v", err)
	}
	if _, err := e.patrol.AddPhoto(ctx, wed2230, bytes.NewReader(jpeg(MaxPhotoBytes)), 0, nil, nil, rc); !errors.Is(err, ErrPhotoTooLarge) {
		t.Fatalf("actual size with a lying header: %v", err)
	}
	if _, err := e.patrol.AddPhoto(ctx, wed2230, bytes.NewReader(jpeg(MaxPhotoBytes-11)), 0, nil, nil, rc); err != nil {
		t.Fatalf("exactly 1 MB must be accepted: %v", err)
	}

	// Bogus coordinates are dropped, not fatal.
	bad := 123.0
	if _, err := e.patrol.AddPhoto(ctx, wed2230, bytes.NewReader(jpeg(10)), 21, &bad, &bad, rc); err != nil {
		t.Fatal(err)
	}

	// Photos 5..10, then the 11th is refused and leaves no file behind.
	for i := 0; i < 6; i++ {
		if _, err := e.patrol.AddPhoto(ctx, wed2230, bytes.NewReader(jpeg(10)), 21, nil, nil, rc); err != nil {
			t.Fatalf("photo %d: %v", i+5, err)
		}
	}
	before := countFiles(t, e.store.Dir)
	if _, err := e.patrol.AddPhoto(ctx, wed2230, bytes.NewReader(jpeg(10)), 21, nil, nil, rc); !errors.Is(err, ErrTooManyPhotos) {
		t.Fatalf("11th photo: %v", err)
	}
	if after := countFiles(t, e.store.Dir); after != before {
		t.Fatalf("rejected photo left a file: %d -> %d", before, after)
	}
	v, _ := e.patrol.Tonight(ctx, wed2230)
	if len(v.Photos) != 10 {
		t.Fatalf("photos %d", len(v.Photos))
	}
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	var walk func(string)
	walk = func(d string) {
		entries, _ := os.ReadDir(d)
		for _, en := range entries {
			if en.IsDir() {
				walk(d + "/" + en.Name())
			} else {
				n++
			}
		}
	}
	walk(dir)
	return n
}

func TestThumbnailOnlyForTheOpenNight(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	item, err := e.patrol.AddPhoto(ctx, wed2230, bytes.NewReader(jpeg(40)), 51, nil, nil, rc)
	if err != nil {
		t.Fatal(err)
	}
	r, ct, err := e.patrol.OpenTonightPhoto(ctx, wed2230.Add(time.Hour), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r)
	r.Close()
	if ct != "image/jpeg" || !bytes.Equal(data, jpeg(40)) {
		t.Fatal("wrong bytes")
	}
	if _, _, err := e.patrol.OpenTonightPhoto(ctx, wed2230, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	// After the window closes, the reporter can no longer see it.
	if _, _, err := e.patrol.OpenTonightPhoto(ctx, time.Date(2026, 10, 8, 6, 0, 0, 0, wed2230.Location()), item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after close: %v", err)
	}
	// Tomorrow night's page must not expose last night's photo either.
	if _, _, err := e.patrol.OpenTonightPhoto(ctx, time.Date(2026, 10, 8, 23, 0, 0, 0, wed2230.Location()), item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("next night: %v", err)
	}
}

func TestRainRequestAndLockedNight(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	if err := e.patrol.RequestRain(ctx, wed2230, rc); err != nil {
		t.Fatal(err)
	}
	n := e.night(wedNight)
	if n.Status != "RAIN_REQUESTED" || !n.RainRequestedAt.Valid || !n.RainRequestedAt.Time.Equal(wed2230) {
		t.Fatalf("%+v", n)
	}
	if err := e.patrol.RequestRain(ctx, wed2230.Add(time.Hour), rc); err != nil {
		t.Fatalf("repeat request must be a no-op: %v", err)
	}
	if got := e.night(wedNight).RainRequestedAt.Time; !got.Equal(wed2230) {
		t.Fatalf("repeat changed the request time: %v", got)
	}
	v, _ := e.patrol.Tonight(ctx, wed2230)
	if !v.RainRequested || v.Locked {
		t.Fatalf("%+v", v)
	}

	// Once an admin confirms the rain, reporters can no longer change anything.
	q := db.New(e.pool)
	if err := q.SetNightStatus(ctx, db.SetNightStatusParams{ID: n.ID, Status: "RAINED_OUT"}); err != nil {
		t.Fatal(err)
	}
	if err := e.patrol.UpdateSituation(ctx, wed2230, "x", rc); !errors.Is(err, ErrNightClosed) {
		t.Fatalf("edit after rain-out: %v", err)
	}
	v, _ = e.patrol.Tonight(ctx, wed2230)
	if !v.Locked || len(v.Candidates) != 0 {
		t.Fatalf("%+v", v)
	}
}

func TestWalkIn(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	if err := e.patrol.AddWalkIn(ctx, wed2230, e.members["dewi"].ID, rc); err != nil {
		t.Fatal(err)
	}
	v, _ := e.patrol.Tonight(ctx, wed2230)
	var dewi *TeamRow
	for i := range v.Team {
		if v.Team[i].MemberID == e.members["dewi"].ID {
			dewi = &v.Team[i]
		}
	}
	if dewi == nil || !dewi.Present || dewi.Source != domain.SourceWalkIn || len(v.Candidates) != 0 {
		t.Fatalf("%+v candidates=%v", dewi, v.Candidates)
	}
	if err := e.patrol.AddWalkIn(ctx, wed2230, e.members["dewi"].ID, rc); !errors.Is(err, ErrAlreadyInTeam) {
		t.Fatalf("twice: %v", err)
	}
	if err := e.patrol.AddWalkIn(ctx, wed2230, e.members["anton"].ID, rc); !errors.Is(err, ErrAlreadyInTeam) {
		t.Fatalf("scheduled member: %v", err)
	}
	// Even toggled absent, a walk-in is never dropped by the team sync.
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["dewi"].ID, false, rc)
	v, _ = e.patrol.Tonight(ctx, wed2230)
	if !strings.Contains(rowNames(v.Team), "Dewi") {
		t.Fatalf("walk-in dropped: %s", rowNames(v.Team))
	}

	NewMemberService(e.pool).SetActive(ctx, e.members["dewi"].ID, false)
	if err := e.patrol.AddWalkIn(ctx, wed2230, 424242, rc); !errors.Is(err, ErrNotInTeam) {
		t.Fatalf("unknown member: %v", err)
	}
}

func TestEveryActionIsAudited(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["anton"].ID, true, rc)
	e.patrol.UpdateSituation(ctx, wed2230, "Aman", rc)
	e.patrol.AddPhoto(ctx, wed2230, bytes.NewReader(jpeg(10)), 21, nil, nil, rc)
	e.patrol.RequestRain(ctx, wed2230, rc)
	e.patrol.AddWalkIn(ctx, wed2230, e.members["dewi"].ID, rc)
	// Failed actions leave no trace.
	e.patrol.UpdateSituation(ctx, wed2230, strings.Repeat("x", 1001), rc)

	n := e.night(wedNight)
	log, err := db.New(e.pool).ListAuditByNight(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, l := range log {
		actions = append(actions, l.Action)
		if l.Actor != "ANONYMOUS" || l.Ip == nil || *l.Ip != rc.IP || l.UserAgent == nil || *l.UserAgent != rc.UserAgent {
			t.Errorf("%s: actor=%s ip=%v ua=%v", l.Action, l.Actor, l.Ip, l.UserAgent)
		}
		var d map[string]any
		json.Unmarshal(l.Detail, &d)
		if d["night_id"] != float64(n.ID) {
			t.Errorf("%s: detail %v lacks night_id", l.Action, d)
		}
	}
	if got := strings.Join(actions, ","); got != "toggle_attendance,update_situation,upload_photo,request_rain,add_walk_in" {
		t.Fatalf("audit actions: %s", got)
	}
}
