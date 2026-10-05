package telegram

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// LockedSource checks that the dedicated advisory-lock connection is alive before
// polling. Losing it terminates the process instead of silently running unlocked.
type LockedSource struct {
	Client     *Client
	Connection *pgx.Conn
}

func (s *LockedSource) Updates(ctx context.Context, offset int64) ([]Update, error) {
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := s.Connection.Ping(check)
	cancel()
	if err != nil {
		return nil, &APIError{Code: 409}
	}
	return s.Client.Updates(ctx, offset)
}
func AcquirePollLock(ctx context.Context, conn *pgx.Conn, botID int64) error {
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, botID).Scan(&locked); err != nil {
		return errors.New("bot lock unavailable")
	}
	if !locked {
		return errors.New("another bot instance is running")
	}
	return nil
}
