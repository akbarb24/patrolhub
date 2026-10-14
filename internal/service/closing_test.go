package service

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/db"
	"github.com/akbarbinaji/patrolhub/internal/domain"
)

type closeEnv struct {
	*patrolEnv
	closer *ClosingService
}

func newCloseEnv(t *testing.T, goLive string) *closeEnv {
	t.Helper()
	e := &closeEnv{patrolEnv: newPatrolEnv(t)}
	e.closer = NewClosingService(e.pool, e.settings)
	if goLive != "" {
		d, err := domain.ParseDate(goLive)
		if err != nil {
			t.Fatal(err)
		}
		set, _ := e.settings.Load(context.Background())
		set.GoLiveDate = &d
		if err := e.settings.Save(context.Background(), set); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *closeEnv) tick(now time.Time) TickResult {
	e.t.Helper()
	res, err := e.closer.Tick(context.Background(), now)
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

// fines returns member label -> "STATUS/fine" for a night.
func (e *closeEnv) fines(date domain.Date) map[string]string {
	e.t.Helper()
	n := e.night(date)
	rows, err := db.New(e.pool).ListAttendanceByNight(context.Background(), n.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range rows {
		out[domain.DisplayName(r.MemberName, r.HouseBlock)] = r.Status + "/" + itoaInt(int(r.FineAmount))
	}
	return out
}

func itoaInt(n int) string { b, _ := json.Marshal(n); return string(b) }

func (e *closeEnv) addPhotos(date domain.Date, n int) {
	e.t.Helper()
	night := e.night(date)
	for i := 0; i < n; i++ {
		_, err := db.New(e.pool).InsertPhoto(context.Background(), db.InsertPhotoParams{
			NightID: night.ID, StorageKey: "k/" + itoaInt(i) + ".jpg", TakenAt: pgTime(testNow), SizeBytes: 10,
		})
		if err != nil {
			e.t.Fatal(err)
		}
	}
}

var afterWed = time.Date(2026, 10, 8, 6, 5, 0, 0, testNow.Location()) // just after Wednesday's window closed

func TestCloseCompleteReport(t *testing.T) {
	e := newCloseEnv(t, "2026-10-01")
	ctx := context.Background()
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["anton"].ID, true, rc)
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["budi"].ID, true, rc)
	e.addPhotos(wedNight, 3)

	res := e.tick(afterWed)
	if res.Closed != 1 {
		t.Fatalf("%+v", res)
	}
	n := e.night(wedNight)
	if n.Status != "REPORTED" || n.NeedsReview || !n.ClosedAt.Valid || n.FineRate == nil || *n.FineRate != 5000 || !n.ClosedAt.Time.Equal(afterWed) {
		t.Fatalf("%+v", n)
	}
	want := map[string]string{"Anton (A-1)": "PRESENT/0", "Budi (B-2)": "PRESENT/0", "C-3": "ABSENT/5000"}
	if got := e.fines(wedNight); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}

	log, _ := db.New(e.pool).ListAuditByNight(ctx, n.ID)
	last := log[len(log)-1]
	if last.Action != "close_night" || last.Actor != "SYSTEM" {
		t.Fatalf("last audit %+v", last)
	}
}

func TestCloseNightNobodyOpened(t *testing.T) {
	e := newCloseEnv(t, "2026-10-01")
	res := e.tick(afterWed) // nobody ever opened Wednesday's page
	if res.Created != 1 || res.Closed != 1 {
		t.Fatalf("%+v", res)
	}
	n := e.night(wedNight)
	if n.Status != "NO_REPORT" || n.NeedsReview || n.FirstReportedAt.Valid {
		t.Fatalf("%+v", n)
	}
	want := map[string]string{"Anton (A-1)": "ABSENT/5000", "Budi (B-2)": "ABSENT/5000", "C-3": "ABSENT/5000"}
	if got := e.fines(wedNight); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestCloseNightWithOnlyAPageViewButNoAction(t *testing.T) {
	e := newCloseEnv(t, "2026-10-01")
	e.patrol.Tonight(context.Background(), wed2230) // viewing is not reporting
	e.tick(afterWed)
	if n := e.night(wedNight); n.Status != "NO_REPORT" {
		t.Fatalf("status %s", n.Status)
	}
}

func TestCloseRainRequested(t *testing.T) {
	e := newCloseEnv(t, "2026-10-01")
	ctx := context.Background()
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["anton"].ID, true, rc)
	e.patrol.RequestRain(ctx, wed2230, rc)
	e.tick(afterWed)
	n := e.night(wedNight)
	if n.Status != "RAIN_REQUESTED" || !n.NeedsReview || !n.ClosedAt.Valid {
		t.Fatalf("%+v", n)
	}
	want := map[string]string{"Anton (A-1)": "PRESENT/0", "Budi (B-2)": "ABSENT/0", "C-3": "ABSENT/0"}
	if got := e.fines(wedNight); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestCloseAdminRainedOut(t *testing.T) {
	e := newCloseEnv(t, "2026-10-01")
	ctx := context.Background()
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["anton"].ID, true, rc)
	n := e.night(wedNight)
	db.New(e.pool).SetNightStatus(ctx, db.SetNightStatusParams{ID: n.ID, Status: "RAINED_OUT"})
	e.tick(afterWed)
	if n := e.night(wedNight); n.Status != "RAINED_OUT" || n.NeedsReview {
		t.Fatalf("%+v", n)
	}
	for who, v := range e.fines(wedNight) {
		if v != "RAINED_OUT/0" {
			t.Errorf("%s: %s", who, v)
		}
	}
}

func TestCloseWithTooFewPhotosNeedsReview(t *testing.T) {
	e := newCloseEnv(t, "2026-10-01")
	e.patrol.ToggleAttendance(context.Background(), wed2230, e.members["anton"].ID, true, rc)
	e.addPhotos(wedNight, 2)
	e.tick(afterWed)
	n := e.night(wedNight)
	if n.Status != "REPORTED" || !n.NeedsReview {
		t.Fatalf("%+v", n)
	}
	if got := e.fines(wedNight)["Budi (B-2)"]; got != "ABSENT/5000" {
		t.Fatalf("fine still applies: %s", got)
	}
}

func TestTickIsIdempotent(t *testing.T) {
	e := newCloseEnv(t, "2026-10-01")
	ctx := context.Background()
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["anton"].ID, true, rc)
	e.addPhotos(wedNight, 3)

	first := e.tick(afterWed)
	snapshot := func() string {
		rows, _ := e.pool.Query(ctx, `SELECT n.patrol_date, n.status, n.closed_at, n.fine_rate, n.needs_review, a.member_id, a.status, a.fine_amount
			FROM patrol_night n LEFT JOIN patrol_attendance a ON a.night_id = n.id ORDER BY n.patrol_date, a.member_id`)
		defer rows.Close()
		var out bytes.Buffer
		for rows.Next() {
			vals, _ := rows.Values()
			b, _ := json.Marshal(vals)
			out.Write(b)
		}
		var audits int
		e.pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'close_night'`).Scan(&audits)
		out.WriteString(itoaInt(audits))
		return out.String()
	}
	before := snapshot()
	second := e.tick(afterWed.Add(10 * time.Minute))
	if second != (TickResult{}) || first.Closed != 1 {
		t.Fatalf("first %+v second %+v", first, second)
	}
	if after := snapshot(); after != before {
		t.Fatalf("second tick changed the data:\n%s\n%s", before, after)
	}
}

func TestFineRateIsFrozenWhenClosed(t *testing.T) {
	e := newCloseEnv(t, "2026-10-01")
	ctx := context.Background()
	e.tick(afterWed) // Wed 7 Oct closes at 5,000

	set, _ := e.settings.Load(ctx)
	set.FinePerAbsence = 10000
	if err := e.settings.Save(ctx, set); err != nil {
		t.Fatal(err)
	}
	nextWed := time.Date(2026, 10, 15, 6, 5, 0, 0, afterWed.Location())
	e.tick(nextWed)

	if got := e.fines(wedNight)["C-3"]; got != "ABSENT/5000" {
		t.Fatalf("old night changed: %s", got)
	}
	if got := e.fines(wedNight.AddDays(7))["C-3"]; got != "ABSENT/10000" {
		t.Fatalf("new night: %s", got)
	}
	if n := e.night(wedNight); *n.FineRate != 5000 {
		t.Fatalf("fine_rate %d", *n.FineRate)
	}
}

func TestTickRespectsGoLiveAndWindow(t *testing.T) {
	ctx := context.Background()

	// Not live yet: nothing at all.
	e := newCloseEnv(t, "")
	if res := e.tick(afterWed); res != (TickResult{}) {
		t.Fatalf("without go_live_date: %+v", res)
	}
	if _, err := db.New(e.pool).GetNightByDate(ctx, pgDate(wedNight)); err == nil {
		t.Fatal("a night was created before go-live")
	}

	// Live from Thursday: Wednesday is left alone even though its window is over.
	e = newCloseEnv(t, "2026-10-08")
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["anton"].ID, true, rc)
	if res := e.tick(afterWed); res.Closed != 0 || e.night(wedNight).ClosedAt.Valid {
		t.Fatalf("closed a night before go-live: %+v", res)
	}

	// Window still open: 05:59 does not close, 06:00 does.
	e = newCloseEnv(t, "2026-10-01")
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["anton"].ID, true, rc)
	if res := e.tick(time.Date(2026, 10, 8, 5, 59, 0, 0, afterWed.Location())); res.Closed != 0 {
		t.Fatalf("closed at 05:59: %+v", res)
	}
	if res := e.tick(time.Date(2026, 10, 8, 6, 0, 0, 0, afterWed.Location())); res.Closed != 1 {
		t.Fatalf("not closed at 06:00: %+v", res)
	}
}

func TestBackfillLooksBackAtMostFourteenDaysAndSkipsDaysWithoutTeam(t *testing.T) {
	e := newCloseEnv(t, "2026-01-01")
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, afterWed.Location()) // a Friday
	res := e.tick(now)
	rows, _ := e.pool.Query(context.Background(), `SELECT patrol_date FROM patrol_night ORDER BY patrol_date`)
	defer rows.Close()
	var dates []string
	for rows.Next() {
		var d time.Time
		rows.Scan(&d)
		dates = append(dates, d.Format("2006-01-02"))
	}
	// Today is 2026-10-30, so 16-29 October are considered. The team works Wednesdays
	// only (21 and 28 October); 7 and 14 October are older than 14 days and stay alone.
	if !reflect.DeepEqual(dates, []string{"2026-10-21", "2026-10-28"}) || res.Created != 2 || res.Closed != 2 {
		t.Fatalf("nights %v, result %+v", dates, res)
	}
}
