// Command server runs the Pairpad API, WebSocket relay and persistence worker.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/faiz-gh/pairpad/server/internal/api"
	"github.com/faiz-gh/pairpad/server/internal/config"
	"github.com/faiz-gh/pairpad/server/internal/hub"
	"github.com/faiz-gh/pairpad/server/internal/store"
	"github.com/faiz-gh/pairpad/server/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return fmt.Errorf("LOG_LEVEL: %w", err)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := connect(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := store.Migrate(ctx, pool, migrations.FS, log); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	st := store.New(pool)
	batcher := store.NewBatcher(st, cfg.FlushInterval, cfg.FlushMaxBatch, log)
	hubs := hub.NewManager(st, batcher, log)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.New(st, hubs, cfg.AllowedOrigins, log).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	// Stop accepting requests, then disconnect peers (hijacked WebSocket
	// connections aren't tracked by http.Server), then flush what's left.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown", "err", err)
	}
	hubs.Close()
	if err := batcher.Close(shutdownCtx); err != nil {
		return fmt.Errorf("final flush: %w", err)
	}
	log.Info("shutdown complete")
	return nil
}

// connect opens a pool and waits for the database to accept connections, so
// the server tolerates starting slightly before Postgres is ready.
func connect(ctx context.Context, url string, log *slog.Logger) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := pool.Ping(ctx)
		if err == nil {
			return pool, nil
		}
		if time.Now().After(deadline) || errors.Is(err, context.Canceled) {
			pool.Close()
			return nil, fmt.Errorf("connect to database: %w", err)
		}
		log.Warn("database not ready; retrying", "err", err)
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
