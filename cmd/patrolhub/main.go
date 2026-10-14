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
	handler, err := httpx.NewRouter(httpx.Deps{
		Config:   cfg,
		Sessions: httpx.NewSessionManager(pool, cfg),
		Settings: settings,
		Admins:   service.NewAdminService(pool),
		Members:  service.NewMemberService(pool),
		Schedule: service.NewScheduleService(pool),
		Patrol:   service.NewPatrolService(pool, settings, storage.LocalStore{Dir: cfg.PhotoDir}),
		Static:   static,
	})
	if err != nil {
		return err
	}
	closer := &job.Closer{Service: service.NewClosingService(pool, settings)}
	go closer.Run(ctx)

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
