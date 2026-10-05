package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"musicgetter/internal/config"
)

var (
	ErrConfiguration = errors.New("invalid database configuration")
	ErrUnavailable   = errors.New("database unavailable")
)

// Open verifies connectivity before returning. Errors deliberately omit the DSN.
func Open(ctx context.Context, cfg config.Database) (*pgxpool.Pool, error) {
	parsed, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, ErrConfiguration
	}
	parsed.MaxConns, parsed.MinConns = cfg.MaxConns, cfg.MinConns
	parsed.MaxConnLifetime, parsed.MaxConnIdleTime = cfg.MaxConnLifetime, cfg.MaxConnIdleTime
	parsed.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	parsed.ConnConfig.RuntimeParams["application_name"] = "musicgetter-backend"
	pool, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		return nil, ErrConfiguration
	}
	checkCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(checkCtx); err != nil {
		pool.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	return pool, nil
}
