package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateValidatesBeforeTouchingDB(t *testing.T) {
	s := NewAdminService(nil)
	if _, err := s.Create(context.Background(), "  ", "long-enough-password"); !errors.Is(err, ErrUsernameRequired) {
		t.Errorf("empty username: %v", err)
	}
	if _, err := s.Create(context.Background(), "admin", "short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("short password: %v", err)
	}
}

// Needs a migrated database (DATABASE_URL); skipped otherwise. It removes its own account.
func TestAdminServiceRoundTrip(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := NewAdminService(pool)

	name := fmt.Sprintf("test-%d", time.Now().UnixNano())
	defer pool.Exec(ctx, "DELETE FROM admin_user WHERE username = $1", name)

	u, err := svc.Create(ctx, " "+name+" ", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != name || u.PasswordHash == "" || u.PasswordHash == "correct horse battery" {
		t.Fatalf("unexpected user %+v", u)
	}
	if _, err := svc.Create(ctx, name, "correct horse battery"); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("duplicate: %v", err)
	}
	if id, err := svc.Authenticate(ctx, name, "correct horse battery"); err != nil || id != u.ID {
		t.Errorf("good login: id=%d err=%v", id, err)
	}
	if _, err := svc.Authenticate(ctx, name, "wrong password!!"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := svc.Authenticate(ctx, name+"-nobody", "correct horse battery"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user: %v", err)
	}
}
