package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/akbarbinaji/patrolhub/internal/config"
	"github.com/akbarbinaji/patrolhub/internal/domain"
	"github.com/akbarbinaji/patrolhub/internal/httpx"
	"github.com/akbarbinaji/patrolhub/internal/job"
	"github.com/akbarbinaji/patrolhub/internal/service"
	"github.com/akbarbinaji/patrolhub/internal/storage"
	"github.com/akbarbinaji/patrolhub/migrations"
	"github.com/akbarbinaji/patrolhub/web"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	setupLogger(cfg)

	switch os.Args[1] {
	case "serve":
		err = serve(cfg)
	case "migrate":
		err = migrate(cfg)
	case "create-admin":
		err = createAdmin(cfg, os.Args[2:])
	case "seed-demo":
		if cfg.AppEnv != "development" {
			err = errors.New("seed-demo only runs when APP_ENV=development")
		} else {
			err = seedDemo(cfg)
		}
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		slog.Error("command failed", "cmd", os.Args[1], "err", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: patrolhub <serve|migrate|create-admin <username>|seed-demo>")
}

func setupLogger(cfg config.Config) {
	var h slog.Handler
	if cfg.IsProduction() {
		h = slog.NewJSONHandler(os.Stderr, nil)
	} else {
		h = slog.NewTextHandler(os.Stderr, nil)
	}
	slog.SetDefault(slog.New(h))
}

func migrate(cfg config.Config) error {
	conn, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(conn, ".")
}

// optionalClock parses an optional HH:MM environment value; empty means unset.
func optionalClock(name, v string) (*domain.Clock, error) {
	if v == "" {
		return nil, nil
	}
	c, err := domain.ParseClock(v)
	if err != nil {
		return nil, fmt.Errorf("%s must be HH:MM, got %q", name, v)
	}
	return &c, nil
}

func serve(cfg config.Config) error {
	if err := migrate(cfg); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	static, err := fs.Sub(web.Static, "static")
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	settings := service.NewSettingsService(pool)
	winOpen, err := optionalClock("WINDOW_OPEN", cfg.WindowOpen)
	if err != nil {
		return err
	}
	winClose, err := optionalClock("WINDOW_CLOSE", cfg.WindowClose)
	if err != nil {
		return err
	}
	if err := settings.SeedWindow(ctx, winOpen, winClose); err != nil {
		return fmt.Errorf("seed window from WINDOW_OPEN/WINDOW_CLOSE: %w", err)
	}
	photos, err := photoStore(ctx, cfg)
	if err != nil {
		return err
	}
	handler, err := httpx.NewRouter(httpx.Deps{
		Config:   cfg,
		Sessions: httpx.NewSessionManager(pool, cfg),
		Settings: settings,
		Admins:   service.NewAdminService(pool),
		Members:  service.NewMemberService(pool),
		Schedule: service.NewScheduleService(pool),
		Patrol:   service.NewPatrolService(pool, settings, photos),
		Nights:   service.NewNightService(pool, settings, photos),
		Fines:    service.NewFineService(pool),
		Recap:    service.NewRecapService(pool, settings),
		Static:   static,
	})
	if err != nil {
		return err
	}
	go (&job.Closer{Service: service.NewClosingService(pool, settings)}).Run(ctx)
	go (&job.Retention{Service: service.NewRetentionService(pool, photos)}).Run(ctx)

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()

	slog.Info("listening", "addr", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// photoStore picks the S3 bucket when BUCKET_NAME is set, the local disk otherwise.
// Startup fails if the bucket is unreachable rather than failing every upload later.
func photoStore(ctx context.Context, cfg config.Config) (storage.PhotoStore, error) {
	if !cfg.S3.Enabled() {
		slog.Info("photo store", "kind", "local", "dir", cfg.PhotoDir)
		return storage.LocalStore{Dir: cfg.PhotoDir}, nil
	}
	s, err := storage.NewS3Store(ctx, cfg.S3)
	if err != nil {
		return nil, fmt.Errorf("photo store: %w", err)
	}
	slog.Info("photo store", "kind", "s3", "endpoint", cfg.S3.Endpoint, "bucket", cfg.S3.Bucket)
	return s, nil
}
