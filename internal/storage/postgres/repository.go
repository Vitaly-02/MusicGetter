package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"musicgetter/internal/domain"
)

// DBTX lets application use cases compose repositories in one pgx transaction.
// Constructors accept either pgxpool.Pool or pgx.Tx; repositories never commit it.
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func repositoryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch databaseError.Code {
		case "23505":
			return domain.ErrConflict
		case "23503", "23514", "23502":
			return domain.ErrInvalid
		}
		if strings.HasPrefix(databaseError.Code, "22") {
			return domain.ErrInvalid
		}
	}
	// Never return raw SQL/DSN/user metadata through the application error boundary.
	return domain.ErrStorage
}

func conflictError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrConflict
	}
	return repositoryError(err)
}

func pageLimit(limit int) error {
	if limit < 1 || limit > 200 {
		return domain.ErrInvalid
	}
	return nil
}
