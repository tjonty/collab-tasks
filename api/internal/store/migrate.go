// Package store is the API's Postgres layer: the schema migrations and the
// queries that sqlc generates from api/db/queries.
package store

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/tjonty/collab-tasks/api/db"
)

// Migrate applies every pending migration. Running it again is a no-op.
// It holds a Postgres advisory lock while it runs, so API instances that
// start together apply each migration once instead of racing.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		return err
	}
	// Poll for the lock every second (goose's default is 5 s) for up to five
	// minutes, so instances waiting on a quick migration start promptly.
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 300))
	if err != nil {
		return err
	}
	// Closing this *sql.DB leaves the pool open.
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations,
		goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
