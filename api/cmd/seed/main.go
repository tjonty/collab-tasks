// Command seed creates a demo project: by default 10,000 tasks with
// dependencies, every status and comments. It writes through the same change
// pipeline as the API, so every change is checked, logged and numbered as if
// a person had made it.
//
//	docker compose exec api /seed            # inside the compose network: ~30 s
//	cd api && go run ./cmd/seed -tasks 1000  # from the host: each change is slower
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tjonty/collab-tasks/api/internal/config"
	"github.com/tjonty/collab-tasks/api/internal/seed"
	"github.com/tjonty/collab-tasks/api/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	tasks := flag.Int("tasks", 10_000, "how many tasks")
	comments := flag.Int("comments", 1_500, "how many comments")
	randomSeed := flag.Uint64("random-seed", 1, "the same random seed plans the same project")
	webURL := flag.String("web", "http://localhost:3000", "the web app's URL, for the link printed at the end")
	flag.Parse()
	if *tasks < 1 || *comments < 0 {
		return errors.New("-tasks must be at least 1 and -comments at least 0")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, config.FromEnv().DatabaseURL)
	if err != nil {
		return fmt.Errorf("postgres config: %w", err)
	}
	defer pool.Close()
	// The API migrates on start, but the seed may run first.
	if err := store.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	plan := seed.NewPlan(*randomSeed, *tasks, *comments)
	fmt.Printf("Seeding %q with %d tasks and %d comments, one change at a time…\n", plan.Project.Name, *tasks, *comments)
	start := time.Now()
	res, err := seed.Run(ctx, pool, plan, func(done, total int) {
		if done%2000 == 0 || done == total {
			fmt.Printf("  %6d / %d changes\n", done, total)
		}
	})
	if err != nil {
		return err
	}

	var size int64
	err = pool.QueryRow(ctx, `SELECT
		(SELECT coalesce(sum(octet_length(row_to_json(t)::text)), 0) FROM tasks t WHERE t.project_id = $1) +
		(SELECT coalesce(sum(octet_length(row_to_json(c)::text)), 0) FROM comments c WHERE c.project_id = $1)`,
		res.ProjectID).Scan(&size)
	if err != nil {
		return fmt.Errorf("measure the project: %w", err)
	}
	elapsed := time.Since(start)
	fmt.Printf("Done in %v: %d changes (%.0f a second), %.1f MB of tasks and comments as JSON.\n",
		elapsed.Round(100*time.Millisecond), res.Changes, float64(res.Changes)/elapsed.Seconds(), float64(size)/1e6)
	fmt.Printf("Open %s/projects/%s\n", *webURL, res.ProjectID)
	return nil
}
