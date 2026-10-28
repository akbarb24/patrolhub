package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akbarbinaji/patrolhub/internal/config"
)

func TestSendGroup(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if r.Method != http.MethodPost || r.URL.Path != "/send/message" || !ok || user != "patrolhub" || pass != "secret" {
			t.Errorf("unexpected request %s %s auth=%v %s/%s", r.Method, r.URL.Path, ok, user, pass)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.Write([]byte(`{"status":200,"code":"SUCCESS"}`))
	}))
	defer srv.Close()

	g := NewGOWA(config.WhatsApp{URL: srv.URL + "/", User: "patrolhub", Password: "secret", GroupJID: "1203@g.us"})
	if err := g.SendGroup(context.Background(), "ABSEN RONDA RT 02"); err != nil {
		t.Fatal(err)
	}
	if got["phone"] != "1203@g.us" || got["message"] != "ABSEN RONDA RT 02" {
		t.Fatalf("body %v", got)
	}
}

func TestSendGroupReportsGatewayErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"code":"INTERNAL_SERVER_ERROR","message":"not logged in"}` + strings.Repeat("x", 500)))
	}))
	defer srv.Close()

	err := NewGOWA(config.WhatsApp{URL: srv.URL, GroupJID: "1203@g.us"}).SendGroup(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("got %v", err)
	}
	if len(err.Error()) > 300 {
		t.Fatalf("error body not truncated: %d bytes", len(err.Error()))
	}
}
