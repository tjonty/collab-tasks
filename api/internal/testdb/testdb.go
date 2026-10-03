// Package testdb runs integration tests against a throwaway Postgres in
// Docker, and creates the rows tests build on. Only tests import it.
package testdb

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// Main starts a Postgres container, applies migrate, points *pool at it and
// runs the package's tests. Call it from TestMain. With -short the tests are
// skipped, so `go test -short ./...` needs no Docker.
//
// Every test shares the one database; tests stay independent by creating
// their own projects with fresh ids.
func Main(m *testing.M, pool **pgxpool.Pool, migrate func(context.Context, *pgxpool.Pool) error) {
	os.Exit(run(m, pool, migrate))
}

func run(m *testing.M, pool **pgxpool.Pool, migrate func(context.Context, *pgxpool.Pool) error) int {
	flag.Parse()
	if testing.Short() {
		fmt.Println("skipping Postgres integration tests in -short mode")
		return 0
	}
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:17-alpine", postgres.BasicWaitStrategies())
	if err != nil {
		fmt.Fprintln(os.Stderr, "start postgres (is Docker running?):", err)
		return 1
	}
	defer testcontainers.TerminateContainer(ctr)

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "connection string:", err)
		return 1
	}
	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		return 1
	}
	defer p.Close()

	if err := migrate(ctx, p); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		return 1
	}
	*pool = p
	return m.Run()
}

// Exec runs one statement.
func Exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) error {
	t.Helper()
	_, err := pool.Exec(context.Background(), sql, args...)
	return err
}

// MustExec runs one statement and fails the test if it errors.
func MustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if err := Exec(t, pool, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// Count runs a query that returns one integer.
func Count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// WantCode fails the test unless err is a Postgres error with code.
func WantCode(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("err = %v, want Postgres error %s", err, code)
	}
	if pgErr.Code != code {
		t.Fatalf("err = %s (%s), want %s", pgErr.Code, pgErr.Message, code)
	}
}

// NewID returns a fresh UUIDv7, like the ids clients generate.
func NewID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

// User inserts a user and returns its id.
func User(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := NewID()
	MustExec(t, pool, `INSERT INTO users (id, display_name) VALUES ($1, 'Ana')`, id)
	return id
}

// Project inserts a project and returns its id.
func Project(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := NewID()
	MustExec(t, pool, `INSERT INTO projects (id, name) VALUES ($1, 'Launch')`, id)
	return id
}

// Task inserts a todo task into projectID and returns its id.
func Task(t *testing.T, pool *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	id := NewID()
	MustExec(t, pool, `INSERT INTO tasks (id, project_id, title) VALUES ($1, $2, 'Task')`, id, projectID)
	return id
}

// Dependency records that taskID depends on dependsOnID.
func Dependency(t *testing.T, pool *pgxpool.Pool, projectID, taskID, dependsOnID uuid.UUID) error {
	t.Helper()
	return Exec(t, pool, `INSERT INTO task_dependencies (project_id, task_id, depends_on_id) VALUES ($1, $2, $3)`,
		projectID, taskID, dependsOnID)
}

// Redis starts a throwaway Redis for one test and returns its URL. The
// test is skipped in -short mode.
func Redis(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker")
	}
	ctx := context.Background()
	ctr, err := tcredis.Run(ctx, "redis:8-alpine")
	if err != nil {
		t.Fatalf("start redis (is Docker running?): %v", err)
	}
	t.Cleanup(func() { testcontainers.TerminateContainer(ctr) })
	url, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return url
}
