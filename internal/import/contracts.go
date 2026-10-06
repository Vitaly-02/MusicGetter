// Package importer defines ingestion and execution boundaries.
package importer

import (
	"context"
	"time"

	"musicgetter/internal/domain"
)

// Every mutation is conditional on the current, unexpired lease generation.
// Claim must run in a short transaction committed before any external work starts.
type JobQueue interface {
	Claim(ctx context.Context, worker string, limit int, lease time.Duration) ([]domain.ImportJob, error)
	Renew(context.Context, domain.ImportJob, time.Duration) (domain.ImportJob, error)
	Complete(context.Context, domain.ImportJob) error
	Retry(ctx context.Context, lease domain.ImportJob, availableAt time.Time, code string) error
	Fail(ctx context.Context, lease domain.ImportJob, code string) error
}
