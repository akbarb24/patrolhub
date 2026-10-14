package service

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/akbarbinaji/patrolhub/internal/db"
)

const (
	bcryptCost        = 12
	minPasswordLength = 10
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrPasswordTooShort   = errors.New("password must be at least 10 characters")
	ErrUsernameRequired   = errors.New("username is required")
	ErrUsernameTaken      = errors.New("username already exists")
)

// dummyHash lets a login for an unknown user cost about as much as one for a known
// user, so response time does not reveal which usernames exist.
var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcryptCost)
	return h
})

type AdminService struct {
	pool *pgxpool.Pool
}

func NewAdminService(pool *pgxpool.Pool) *AdminService {
	return &AdminService{pool: pool}
}

// Create adds an admin account with a bcrypt-hashed password.
func (s *AdminService) Create(ctx context.Context, username, password string) (db.AdminUser, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return db.AdminUser{}, ErrUsernameRequired
	}
	if len(password) < minPasswordLength {
		return db.AdminUser{}, ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return db.AdminUser{}, err
	}
	u, err := db.New(s.pool).CreateAdmin(ctx, db.CreateAdminParams{Username: username, PasswordHash: string(hash)})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return db.AdminUser{}, ErrUsernameTaken
	}
	return u, err
}

// Authenticate returns the admin id, or ErrInvalidCredentials for both an unknown
// user and a wrong password.
func (s *AdminService) Authenticate(ctx context.Context, username, password string) (int64, error) {
	u, err := db.New(s.pool).GetAdminByUsername(ctx, strings.TrimSpace(username))
	if errors.Is(err, pgx.ErrNoRows) {
		bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
		return 0, ErrInvalidCredentials
	}
	if err != nil {
		return 0, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return 0, ErrInvalidCredentials
	}
	return u.ID, nil
}
