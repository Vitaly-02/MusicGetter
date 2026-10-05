// Package importer defines ingestion and execution boundaries.
package importer

import (
	"context"
	"time"

	"musicgetter/internal/domain"
)

// OwnerID must be supplied from authenticated server context, not client JSON.
type AppendBatch struct {
	OwnerID       domain.ID
	CaptureID     domain.ID
	Sequence      int64
	PayloadDigest string
	Items         []domain.CollectionItem
}

type BatchReceipt struct {
	Sequence      int64
	Replay        bool
	AcceptedItems int64
}

type SealCapture struct {
	OwnerID      domain.ID
	CaptureID    domain.ID
	LastSequence int64               // -1 for an empty capture
	State        domain.CaptureState // only sealed_partial or sealed_complete
	Reason       string
}

// Ingestion persists a batch atomically. Replay with a different digest conflicts.
// Seal validates contiguous sequences and atomically creates the import + jobs.
type Ingestion interface {
	Append(context.Context, AppendBatch) (BatchReceipt, error)
	Seal(context.Context, SealCapture) (domain.ID, error)
}

type JobLease struct {
	JobID      domain.ID
	ImportID   domain.ID
	Kind       string
	Generation int64
	ExpiresAt  time.Time
}

// Every mutation is conditional on the current, unexpired lease generation.
// Claim commits its short transaction before any external work starts.
type JobQueue interface {
	Claim(ctx context.Context, worker string, limit int, lease time.Duration) ([]JobLease, error)
	Renew(context.Context, JobLease, time.Duration) (JobLease, error)
	Complete(context.Context, JobLease) error
	Retry(ctx context.Context, lease JobLease, availableAt time.Time, code string) error
	Fail(ctx context.Context, lease JobLease, code string) error
}
