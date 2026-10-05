package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	"musicgetter/migrations"
)

var ErrSchema = errors.New("database schema is not ready")

// Ready checks both connectivity and the schema expected by this build.
// It is read-only: starting an HTTP replica never runs migrations.
func Ready(ctx context.Context, pool *pgxpool.Pool) error {
	var version int64
	var namespace bool
	err := pool.QueryRow(ctx, `SELECT COALESCE(MAX(version_id), 0),
		to_regnamespace('musicgetter') IS NOT NULL
		FROM public.goose_db_version WHERE is_applied`).Scan(&version, &namespace)
	if err != nil {
		return ErrUnavailable
	}
	if version != migrations.Version || !namespace {
		return ErrSchema
	}
	return nil
}
