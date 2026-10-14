package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/akbarbinaji/patrolhub/internal/db"
)

func TestRetentionDeletesOldPhotos(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	rs := NewRetentionService(e.pool, e.store)
	q := db.New(e.pool)

	// A recent photo (uploaded for real), an old one with its file, and an old one whose file is already gone.
	recent, err := e.patrol.AddPhoto(ctx, wed2230, bytes.NewReader(jpeg(10)), 21, nil, nil, rc)
	if err != nil {
		t.Fatal(err)
	}
	night := e.night(wedNight)
	mk := func(key string, age int) db.PatrolPhoto {
		p, err := q.InsertPhoto(ctx, db.InsertPhotoParams{NightID: night.ID, StorageKey: key, TakenAt: pgTime(testNow), SizeBytes: 1})
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.pool.Exec(ctx, `UPDATE patrol_photo SET created_at = $2 WHERE id = $1`, p.ID, testNow.AddDate(0, 0, -age))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	withFile := mk("2026/01/01/old.jpg", PhotoRetentionDays+1)
	if err := os.MkdirAll(filepath.Join(e.store.Dir, "2026/01/01"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.store.Dir, "2026/01/01/old.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := mk("2026/01/02/gone.jpg", PhotoRetentionDays+30)
	young := mk("2026/05/01/young.jpg", PhotoRetentionDays-1)

	n, err := rs.Run(ctx, testNow)
	if err != nil || n != 2 {
		t.Fatalf("removed %d, err %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(e.store.Dir, "2026/01/01/old.jpg")); !os.IsNotExist(err) {
		t.Fatal("old file still on disk")
	}
	for _, p := range []db.PatrolPhoto{withFile, missing} {
		if _, err := q.GetPhoto(ctx, p.ID); err == nil {
			t.Errorf("row %d still exists", p.ID)
		}
	}
	for _, id := range []int64{recent.ID, young.ID} {
		if _, err := q.GetPhoto(ctx, id); err != nil {
			t.Errorf("photo %d was removed too early: %v", id, err)
		}
	}
	if n, err := rs.Run(ctx, testNow); err != nil || n != 0 {
		t.Fatalf("second run removed %d, err %v", n, err)
	}
}

func TestRetentionKeepsRowWhenFileCannotBeDeleted(t *testing.T) {
	e := newPatrolEnv(t)
	ctx := context.Background()
	q := db.New(e.pool)
	e.patrol.Tonight(ctx, wed2230)
	night := e.night(wedNight)

	// A non-empty directory under the key makes os.Remove fail with something other than "not found".
	dir := filepath.Join(e.store.Dir, "2026/01/03/stuck.jpg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inner"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := q.InsertPhoto(ctx, db.InsertPhotoParams{NightID: night.ID, StorageKey: "2026/01/03/stuck.jpg", TakenAt: pgTime(testNow), SizeBytes: 1})
	e.pool.Exec(ctx, `UPDATE patrol_photo SET created_at = $2 WHERE id = $1`, p.ID, testNow.AddDate(0, 0, -400))

	n, err := NewRetentionService(e.pool, e.store).Run(ctx, testNow)
	if err == nil || n != 0 {
		t.Fatalf("removed %d, err %v", n, err)
	}
	if _, err := q.GetPhoto(ctx, p.ID); err != nil {
		t.Fatalf("row must stay so the next run retries: %v", err)
	}
}
