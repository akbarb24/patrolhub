package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/akbarbinaji/patrolhub/internal/db"
)

type fakeMessenger struct {
	sent []string
	err  error
}

func (f *fakeMessenger) SendGroup(_ context.Context, text string) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, text)
	return nil
}

func newReportEnv(t *testing.T) (*closeEnv, *ReportService, *fakeMessenger) {
	e := newCloseEnv(t, "2026-10-01")
	msg := &fakeMessenger{}
	return e, NewReportService(e.pool, e.settings, msg, "https://ronda.example.com/"), msg
}

func TestReportSentOnceAfterClose(t *testing.T) {
	e, reports, msg := newReportEnv(t)
	ctx := context.Background()
	e.patrol.ToggleAttendance(ctx, wed2230, e.members["anton"].ID, true, rc)
	e.patrol.UpdateSituation(ctx, wed2230, "Aman", rc)
	e.addPhotos(wedNight, 3)
	e.tick(afterWed)

	if err := reports.Tick(ctx, afterWed); err != nil {
		t.Fatal(err)
	}
	n := e.night(wedNight)
	if len(msg.sent) != 1 || n.ShareToken == nil || !n.WaSentAt.Valid {
		t.Fatalf("sent %d, night %+v", len(msg.sent), n)
	}
	want := strings.Join([]string{
		"ABSEN RONDA RT 02", "Rabu, 7 Okt 2026",
		"- Anton (A-1): hadir", "- Budi (B-2): absen", "- C-3: absen",
		"Situasi:", "Aman", "Foto:", "https://ronda.example.com/shared/" + *n.ShareToken, "",
	}, "\n")
	if msg.sent[0] != want {
		t.Fatalf("got:\n%s\nwant:\n%s", msg.sent[0], want)
	}

	if err := reports.Tick(ctx, afterWed.Add(5*time.Minute)); err != nil || len(msg.sent) != 1 {
		t.Fatalf("second tick sent again: %d %v", len(msg.sent), err)
	}
}

func TestReportRetriedAfterFailedSend(t *testing.T) {
	e, reports, msg := newReportEnv(t)
	ctx := context.Background()
	e.tick(afterWed)

	msg.err = errors.New("gateway down")
	if err := reports.Tick(ctx, afterWed); err == nil {
		t.Fatal("expected error")
	}
	if n := e.night(wedNight); n.WaSentAt.Valid {
		t.Fatal("marked sent although the send failed")
	}

	msg.err = nil
	if err := reports.Tick(ctx, afterWed.Add(5*time.Minute)); err != nil || len(msg.sent) != 1 {
		t.Fatalf("retry: %d %v", len(msg.sent), err)
	}
	// No photos: no link, no token.
	if !strings.Contains(msg.sent[0], "Status: Tanpa laporan") || !strings.HasSuffix(msg.sent[0], "Foto: -\n") {
		t.Fatalf("text:\n%s", msg.sent[0])
	}
	if n := e.night(wedNight); n.ShareToken != nil {
		t.Fatalf("token without photos: %s", *n.ShareToken)
	}
}

func TestReportSkipsNightsClosedLongAgo(t *testing.T) {
	e, reports, msg := newReportEnv(t)
	e.tick(afterWed)
	if err := reports.Tick(context.Background(), afterWed.Add(25*time.Hour)); err != nil || len(msg.sent) != 0 {
		t.Fatalf("sent %d %v", len(msg.sent), err)
	}
}

func TestSharedPhotos(t *testing.T) {
	e, reports, _ := newReportEnv(t)
	ctx := context.Background()
	nights := NewNightService(e.pool, e.settings, e.store)
	e.patrol.Tonight(ctx, wed2230) // creates the night
	e.addPhotos(wedNight, 2)
	if err := e.store.Save(ctx, "k/0.jpg", bytes.NewReader(jpeg(10))); err != nil {
		t.Fatal(err)
	}
	e.tick(afterWed)
	if err := reports.Tick(ctx, afterWed); err != nil {
		t.Fatal(err)
	}
	token := *e.night(wedNight).ShareToken

	date, ids, err := nights.SharedPhotos(ctx, token)
	if err != nil || date != wedNight || len(ids) != 2 {
		t.Fatalf("%v %v %v", date, ids, err)
	}
	f, ct, err := nights.OpenSharedPhoto(ctx, token, ids[0])
	if err != nil || ct != "image/jpeg" {
		t.Fatalf("%q %v", ct, err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if !bytes.Equal(got, jpeg(10)) {
		t.Fatal("wrong photo bytes")
	}

	// A photo of another night is not reachable through this link.
	if err := db.New(e.pool).InsertNightIfMissing(ctx, pgDate(wedNight.AddDays(7))); err != nil {
		t.Fatal(err)
	}
	next := e.night(wedNight.AddDays(7))
	p, err := db.New(e.pool).InsertPhoto(ctx, db.InsertPhotoParams{NightID: next.ID, StorageKey: "k/x.jpg", TakenAt: pgTime(testNow), SizeBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := nights.OpenSharedPhoto(ctx, token, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other night's photo: %v", err)
	}

	for _, bad := range []string{"", "nope", strings.ToLower(token), "AAAAAAAAAAAAAAAAAAAAAAAAAA"} {
		if _, _, err := nights.SharedPhotos(ctx, bad); !errors.Is(err, ErrNotFound) {
			t.Errorf("token %q: %v", bad, err)
		}
	}
}
