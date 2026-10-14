package httpx

import (
	"net/http"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akbarbinaji/patrolhub/internal/config"
)

// NewSessionManager creates the admin session manager backed by the sessions table.
func NewSessionManager(pool *pgxpool.Pool, cfg config.Config) *scs.SessionManager {
	sm := scs.New()
	sm.Store = pgxstore.New(pool)
	sm.Lifetime = 14 * 24 * time.Hour
	sm.Cookie.HttpOnly = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Secure = cfg.IsProduction()
	return sm
}
