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

	"github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/akbarbinaji/patrolhub/internal/config"
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
		err = errors.New("create-admin: not implemented yet (step 5.1)")
	case "seed-demo":
		if cfg.AppEnv != "development" {
			err = errors.New("seed-demo only runs when APP_ENV=development")
		} else {
			err = errors.New("seed-demo: not implemented yet (step 6.4)")
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
	// No migrations exist until step 2 adds the first one.
	if err := goose.Up(conn, "."); err != nil && !errors.Is(err, goose.ErrNoMigrationFiles) {
		return err
	}
	return nil
}

func serve(cfg config.Config) error {
	if err := migrate(cfg); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	static, err := fs.Sub(web.Static, "static")
	if err != nil {
		return err
	}
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServerFS(static)))

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: r, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
