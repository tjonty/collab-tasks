// Command server runs the Collab Tasks HTTP API.
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
	"github.com/redis/go-redis/v9"

	"github.com/tjonty/collab-tasks/api/internal/config"
	"github.com/tjonty/collab-tasks/api/internal/health"
	"github.com/tjonty/collab-tasks/api/internal/httpapi"
	"github.com/tjonty/collab-tasks/api/internal/store"
)

const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.FromEnv()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("postgres config: %w", err)
	}
	defer pool.Close()

	// The schema must be current before any request runs, so an unreachable
	// Postgres at startup is fatal. Compose starts the API only once
	// Postgres is healthy.
	if err := store.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	slog.Info("migrations applied")

	rdb, err := newRedisClient(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("redis config: %w", err)
	}
	defer rdb.Close()

	checks := map[string]health.Pinger{
		"postgres": pool,
		"redis": health.PingerFunc(func(ctx context.Context) error {
			return rdb.Ping(ctx).Err()
		}),
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.NewRouter(checks, cfg.CORSOrigin),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// newRedisClient builds a client that honours context deadlines, so a hung
// Redis cannot hold /healthz past its per-check timeout. go-redis ignores
// them by default and waits out its own 5s ReadTimeout instead.
func newRedisClient(url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opts.ContextTimeoutEnabled = true
	return redis.NewClient(opts), nil
}
