package store

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationLockID is an arbitrary constant used with pg_advisory_lock so that
// concurrently starting servers don't apply migrations twice.
const migrationLockID = 727_273_001

// Migrate applies every *.sql file in migrations that has not been applied
// yet, in lexical order, each in its own transaction.
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS, log *slog.Logger) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockID) //nolint:errcheck

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrations, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")

		var applied bool
		if err := conn.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", version,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check %s: %w", version, err)
		}
		if applied {
			continue
		}

		sql, err := fs.ReadFile(migrations, name)
		if err != nil {
			return err
		}
		if err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version)
			return err
		}); err != nil {
			return fmt.Errorf("apply %s: %w", version, err)
		}
		log.Info("applied migration", "version", version)
	}
	return nil
}
