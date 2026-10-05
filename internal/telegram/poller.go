package telegram

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type UpdateSource interface {
	Updates(context.Context, int64) ([]Update, error)
}
type UpdateStore interface {
	ClaimUpdate(context.Context, int64, int64) (bool, error)
}
type UpdateHandler interface {
	Handle(context.Context, Update) error
}

// Poll runs sequentially. Claim errors stop the process before acknowledging the
// update. Handler/send failures are not replayed (external delivery is uncertain).
func Poll(ctx context.Context, source UpdateSource, store UpdateStore, handler UpdateHandler, botID int64, logger *slog.Logger) error {
	var offset int64
	delay := time.Second
	for {
		updates, err := source.Updates(ctx, offset)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) && (apiErr.Code == 401 || apiErr.Code == 403 || apiErr.Code == 409) {
				return err
			}
			wait := delay
			if apiErr != nil && apiErr.RetryAfter > wait {
				wait = apiErr.RetryAfter
			}
			logger.WarnContext(ctx, "telegram_poll_failed")
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
			delay = min(delay*2, 30*time.Second)
			continue
		}
		delay = time.Second
		for _, update := range updates {
			if update.ID < 0 || update.ID < offset {
				continue
			}
			step, cancel := context.WithTimeout(ctx, 15*time.Second)
			claimed, err := store.ClaimUpdate(step, botID, update.ID)
			if err != nil {
				cancel()
				return err
			}
			if claimed {
				if err = handleSafely(step, handler, update); err != nil && ctx.Err() == nil {
					logger.ErrorContext(step, "telegram_command_failed")
				}
			}
			cancel()
			if ctx.Err() != nil {
				return nil
			}
			offset = update.ID + 1
		}
	}
}
func handleSafely(ctx context.Context, h UpdateHandler, u Update) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("Telegram handler panicked")
		}
	}()
	return h.Handle(ctx, u)
}
