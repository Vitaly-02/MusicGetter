package postgres

import (
	"context"
	"errors"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"musicgetter/internal/config"
)

// Migrate uses a separate connection and an advisory lock, never the server pool.
// Up applies all pending migrations; down rolls back exactly one migration.
func Migrate(ctx context.Context, cfg config.Database, files fs.FS, direction string) error {
	if direction != "up" && direction != "down" {
		return errors.New("migration direction must be up or down")
	}
	parsed, err := pgx.ParseConfig(cfg.URL)
	if err != nil {
		return ErrConfiguration
	}
	parsed.ConnectTimeout = cfg.ConnectTimeout
	parsed.RuntimeParams["application_name"] = "musicgetter-migrate"
	db := stdlib.OpenDB(*parsed)
	defer db.Close()
	db.SetMaxOpenConns(2)
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return errors.New("migration lock initialization failed")
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, files,
		goose.WithSessionLocker(locker), goose.WithTableName("public.goose_db_version"))
	if err != nil {
		return errors.New("invalid migration files")
	}
	if direction == "up" {
		_, err = provider.Up(ctx)
	} else {
		_, err = provider.Down(ctx)
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Library errors can include connection strings or SQL; keep the public/log
		// error safe. Troubleshoot detailed server errors in PostgreSQL logs.
		return errors.New("migration failed; inspect PostgreSQL logs")
	}
	return nil
}
