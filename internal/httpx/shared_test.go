package httpx

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestSharedPhotoLink(t *testing.T) {
	a := newApp(t, wibTime(2026, 10, 8, 7, 0))
	ctx := context.Background()
	const token = "ABCDEFGHIJKLMNOPQRSTUVWXYZ" // 26 base32 characters, like rand.Text
	var shared, other int64
	if err := a.pool.QueryRow(ctx, `INSERT INTO patrol_night (patrol_date, status, closed_at, share_token)
		VALUES ('2026-10-07', 'REPORTED', now(), $1) RETURNING id`, token).Scan(&shared); err != nil {
		t.Fatal(err)
	}
	if err := a.pool.QueryRow(ctx, `INSERT INTO patrol_night (patrol_date) VALUES ('2026-10-08') RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	photo := func(night int64, key string) int64 {
		var id int64
		if err := a.pool.QueryRow(ctx, `INSERT INTO patrol_photo (night_id, storage_key, taken_at, size_bytes)
			VALUES ($1, $2, now(), 4) RETURNING id`, night, key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	mine, missing, foreign := photo(shared, "a/1.jpg"), photo(shared, "a/2.jpg"), photo(other, "b/1.jpg")
	for _, key := range []string{"a/1.jpg", "b/1.jpg"} {
		if err := a.photos.Save(ctx, key, bytes.NewReader([]byte{0xFF, 0xD8, 0xFF, 0xE0})); err != nil {
			t.Fatal(err)
		}
	}

	b := a.anonymous()
	resp, body := b.do("GET", "/shared/"+token, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Robots-Tag") == "" {
		t.Fatalf("page: %d %v", resp.StatusCode, resp.Header)
	}
	contains(t, body, "Rabu, 7 Oktober 2026", fmt.Sprintf("/shared/%s/photos/%d", token, mine))
	notContains(t, body, fmt.Sprintf("/photos/%d", foreign))

	resp, _ = b.do("GET", fmt.Sprintf("/shared/%s/photos/%d", token, mine), nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("photo: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	for _, path := range []string{
		"/shared/ZZZZZZZZZZZZZZZZZZZZZZZZZZ",                              // unknown token
		"/shared/nope",                                                    // malformed token
		fmt.Sprintf("/shared/%s/photos/%d", token, foreign),               // another night's photo
		fmt.Sprintf("/shared/%s/photos/%d", token, missing),               // file already deleted
		fmt.Sprintf("/shared/ZZZZZZZZZZZZZZZZZZZZZZZZZZ/photos/%d", mine), // right photo, wrong token
	} {
		if resp, _ := b.do("GET", path, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, resp.StatusCode)
		}
	}
}
