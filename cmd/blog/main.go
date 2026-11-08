package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FlavRomano/blog/internal/auth"
	"github.com/FlavRomano/blog/internal/clientip"
	"github.com/FlavRomano/blog/internal/config"
	"github.com/FlavRomano/blog/internal/database"
	"github.com/FlavRomano/blog/internal/database/sqlc"
	"github.com/FlavRomano/blog/internal/scheduler"
	"github.com/FlavRomano/blog/internal/server"
)

func main() {
	var err error
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		err = runAdmin(os.Args[2:])
	} else {
		err = run()
	}
	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// newAuth returns nil when no TOTP key is configured (admin disabled).
func newAuth(cfg config.Config, db *sql.DB) (*auth.Service, error) {
	if cfg.TOTPKeyHex == "" {
		return nil, nil
	}
	keys, err := auth.NewKeys(cfg.TOTPKeyHex)
	if err != nil {
		return nil, err
	}
	return auth.NewService(db, keys, auth.Options{Issuer: cfg.SiteName}), nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	if err := os.MkdirAll(cfg.UploadsDir, 0o750); err != nil {
		return err
	}
	db, err := database.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	authSvc, err := newAuth(cfg, db)
	if err != nil {
		return err
	}
	deps := server.Deps{DB: db, Log: log, Cfg: cfg}
	tasks := []scheduler.Task{{
		Name: "publish-scheduled",
		Run: func(ctx context.Context) error {
			n, err := sqlc.New(db).PublishDuePosts(ctx, sql.NullString{String: time.Now().UTC().Format(auth.TimeFormat), Valid: true})
			if n > 0 {
				log.Info("published scheduled posts", "count", n)
			}
			return err
		},
	}}
	if authSvc != nil {
		if cfg.IPHashSalt == "" {
			return errors.New("BLOG_IP_SALT is required when the admin is enabled; generate with: openssl rand -hex 32")
		}
		deps.Auth = authSvc
		deps.IP = clientip.New(cfg.IPHashSalt, cfg.TrustTunnel)
		tasks = append(tasks, scheduler.Task{Name: "prune-auth", Run: authSvc.Prune})
	}

	handler, err := server.New(deps)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go scheduler.Run(ctx, time.Minute, log, tasks...)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Info("listening", "addr", cfg.Addr, "admin", authSvc != nil)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func usage() error {
	return fmt.Errorf("usage: blog admin <setup|reset>\n  setup  create the admin account (fails if one exists)\n  reset  replace the password, TOTP secret and recovery codes; ends all sessions")
}
