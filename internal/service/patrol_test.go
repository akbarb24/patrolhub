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

// photos uploads n small JPEGs for the night open at `at`.
func (e *patrolEnv) photos(at time.Time, n int) {
	e.t.Helper()
	for i := 0; i < n; i++ {
		if _, err := e.patrol.AddPhoto(context.Background(), at, bytes.NewReader(jpeg(10)), 21, nil, nil, rc); err != nil {
			e.t.Fatalf("photo %d: %v", i, err)
		}
	}
}

// submit uploads the minimum photos and submits the report with the given members present.
func (e *patrolEnv) submit(at time.Time, note string, present ...string) {
	e.t.Helper()
	e.photos(at, 3)
	in := SubmitInput{Note: note, Present: e.ids(present...)}
	if err := e.patrol.Submit(context.Background(), at, in, rc); err != nil {
		e.t.Fatalf("submit: %v", err)
	}
}

func (e *patrolEnv) ids(keys ...string) []int64 {
	out := make([]int64, len(keys))
	for i, k := range keys {
		out[i] = e.members[k].ID
	}
	return out
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
	if err := e.patrol.Submit(ctx, wed2100, SubmitInput{Note: "x"}, rc); !errors.Is(err, ErrWindowClosed) {
		t.Fatalf("submit outside window: %v", err)
	}
}

func TestSubmitSavesReportAndLocksIt(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	v, err := e.patrol.Tonight(ctx, wed2230)
	if err != nil || !v.Open || v.Locked || v.Summary() || len(v.Team) != 3 {
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

	// A photo is a draft: it does not submit the report.
	e.photos(wed2230, 1)
	if n := e.night(wedNight); !n.FirstReportedAt.Valid || n.SubmittedAt.Valid {
		t.Fatalf("after a photo: first=%+v submitted=%+v", n.FirstReportedAt, n.SubmittedAt)
	}
	e.photos(wed2230, 2)

	at := wed2230.Add(30 * time.Minute)
	in := SubmitInput{Note: "  Aman, lampu pos mati.  ", Present: e.ids("anton", "anton")}
	if err := e.patrol.Submit(ctx, at, in, rc); err != nil {
		t.Fatal(err)
	}
	n := e.night(wedNight)
	if !n.SubmittedAt.Valid || !n.SubmittedAt.Time.Equal(at) || !n.FirstReportedAt.Time.Equal(wed2230) || n.SituationNote != "Aman, lampu pos mati." {
		t.Fatalf("%+v", n)
	}
	v, _ = e.patrol.Tonight(ctx, wed2230.Add(time.Hour))
	if !v.Locked || !v.Submitted || !v.SubmittedAt.Equal(at) || !v.Summary() || len(v.Candidates) != 0 || v.UntilClose <= 0 {
		t.Fatalf("%+v", v)
	}
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

	// Nothing can change afterwards.
	if err := e.patrol.Submit(ctx, at, SubmitInput{Note: "lagi", Present: e.ids("budi")}, rc); !errors.Is(err, ErrNightClosed) {
		t.Fatalf("second submit: %v", err)
	}
	if _, err := e.patrol.AddPhoto(ctx, at, bytes.NewReader(jpeg(10)), 21, nil, nil, rc); !errors.Is(err, ErrNightClosed) {
		t.Fatalf("photo after submit: %v", err)
	}
	if err := e.patrol.RequestRain(ctx, at, rc); !errors.Is(err, ErrNightClosed) {
		t.Fatalf("rain after submit: %v", err)
	}
}

func TestSubmitValidation(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	e.photos(wed2230, 2)
	if err := e.patrol.Submit(ctx, wed2230, SubmitInput{Note: "Aman"}, rc); !errors.Is(err, ErrTooFewPhotos) {
		t.Fatalf("two photos: %v", err)
	}
	e.photos(wed2230, 1)
	for _, tc := range []struct {
		name string
		in   SubmitInput
		want error
	}{
		{"empty note", SubmitInput{Note: "   "}, ErrNoSituation},
		{"note too long", SubmitInput{Note: strings.Repeat("é", 1001)}, ErrTooLong},
		{"not on the team", SubmitInput{Note: "Aman", Present: e.ids("dewi")}, ErrNotInTeam},
		{"unknown member", SubmitInput{Note: "Aman", Present: []int64{99999}}, ErrNotInTeam},
		{"walk-in already on team", SubmitInput{Note: "Aman", WalkIns: e.ids("anton")}, ErrAlreadyInTeam},
		{"unknown walk-in", SubmitInput{Note: "Aman", WalkIns: []int64{424242}}, ErrNotInTeam},
	} {
		if err := e.patrol.Submit(ctx, wed2230, tc.in, rc); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
	// A rejected submit changes nothing.
	if n := e.night(wedNight); n.SubmittedAt.Valid || n.SituationNote != "" {
		t.Fatalf("%+v", n)
	}
	if err := e.patrol.Submit(ctx, wed2230, SubmitInput{Note: strings.Repeat("é", 1000)}, rc); err != nil {
		t.Fatalf("exactly 1000 characters must be accepted: %v", err)
	}
}

func TestReportAfterMidnightBelongsToPreviousNight(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	e.submit(thu0030, "Aman", "budi")
	if n := e.night(wedNight); !n.SubmittedAt.Valid {
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
	e.submit(wed2230, "Aman", "anton")

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
	// A submitted night is not synced on view; the closer syncs it right before closing.
	if err := WithTx(ctx, e.pool, func(q *db.Queries) error { return syncTeam(ctx, q, e.night(wedNight)) }); err != nil {
		t.Fatal(err)
	}
	rows, _ := attendanceRows(ctx, db.New(e.pool), e.night(wedNight).ID)
	if rowNames(rows) != "Anton (A-1),C-3,Dewi (D-4)" {
		t.Fatalf("team %s", rowNames(rows))
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
	if err := e.patrol.RequestRain(ctx, wed2230.Add(time.Hour), rc); !errors.Is(err, ErrNightClosed) {
		t.Fatalf("repeat request: %v", err)
	}
	if got := e.night(wedNight).RainRequestedAt.Time; !got.Equal(wed2230) {
		t.Fatalf("repeat changed the request time: %v", got)
	}
	// A rain request locks the report; the page shows the summary.
	v, _ := e.patrol.Tonight(ctx, wed2230)
	if !v.RainRequested || !v.Locked || v.Submitted || !v.Summary() {
		t.Fatalf("%+v", v)
	}
	if err := e.patrol.Submit(ctx, wed2230, SubmitInput{Note: "x"}, rc); !errors.Is(err, ErrNightClosed) {
		t.Fatalf("submit after rain request: %v", err)
	}

	// Once an admin confirms the rain, reporters can no longer change anything.
	q := db.New(e.pool)
	if err := q.SetNightStatus(ctx, db.SetNightStatusParams{ID: n.ID, Status: "RAINED_OUT"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.patrol.AddPhoto(ctx, wed2230, bytes.NewReader(jpeg(10)), 21, nil, nil, rc); !errors.Is(err, ErrNightClosed) {
		t.Fatalf("photo after rain-out: %v", err)
	}
	v, _ = e.patrol.Tonight(ctx, wed2230)
	if !v.Locked || v.Summary() || len(v.Candidates) != 0 {
		t.Fatalf("%+v", v)
	}
}

func TestWalkInViaSubmit(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	e.photos(wed2230, 3)
	in := SubmitInput{Note: "Aman", Present: e.ids("anton"), WalkIns: e.ids("dewi", "dewi")}
	if err := e.patrol.Submit(ctx, wed2230, in, rc); err != nil {
		t.Fatal(err)
	}
	v, _ := e.patrol.Tonight(ctx, wed2230)
	var dewi *TeamRow
	for i := range v.Team {
		if v.Team[i].MemberID == e.members["dewi"].ID {
			dewi = &v.Team[i]
		}
	}
	if dewi == nil || !dewi.Present || dewi.Source != domain.SourceWalkIn || len(v.Team) != 4 {
		t.Fatalf("%+v team=%s", dewi, rowNames(v.Team))
	}
}

func TestInactiveMemberCannotWalkIn(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	e.photos(wed2230, 3)
	NewMemberService(e.pool).SetActive(ctx, e.members["dewi"].ID, false)
	if err := e.patrol.Submit(ctx, wed2230, SubmitInput{Note: "Aman", WalkIns: e.ids("dewi")}, rc); !errors.Is(err, ErrNotInTeam) {
		t.Fatalf("inactive walk-in: %v", err)
	}
}

func TestEveryActionIsAudited(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	e.photos(wed2230, 3)
	// Failed actions leave no trace.
	e.patrol.Submit(ctx, wed2230, SubmitInput{Note: "Aman", Present: e.ids("dewi")}, rc)
	if err := e.patrol.Submit(ctx, wed2230, SubmitInput{Note: "Aman", Present: e.ids("anton"), WalkIns: e.ids("dewi")}, rc); err != nil {
		t.Fatal(err)
	}

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
	if got := strings.Join(actions, ","); got != "upload_photo,upload_photo,upload_photo,submit_report" {
		t.Fatalf("audit actions: %s", got)
	}
}
