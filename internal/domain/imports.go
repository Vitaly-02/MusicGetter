package domain

import "time"

type Import struct {
	ID                      ID
	OwnerID                 ID
	RequestKey              string
	SourceCollectionID      ID
	ProfileID               ID
	Source                  Source
	DestinationCollectionID ID
	ConnectionID            ID
	State                   ImportState
	CreatedAt               time.Time
}

type ImportItemState string

const (
	ItemPending        ImportItemState = "pending"
	ItemSearching      ImportItemState = "searching"
	ItemNeedsReview    ImportItemState = "needs_review"
	ItemMatched        ImportItemState = "matched"
	ItemEnsuring       ImportItemState = "ensuring"
	ItemReconciling    ImportItemState = "reconciling"
	ItemAdded          ImportItemState = "added"
	ItemAlreadyPresent ImportItemState = "already_present"
	ItemSkipped        ImportItemState = "skipped"
	ItemFailed         ImportItemState = "failed"
	ItemCancelled      ImportItemState = "cancelled"
)

type ImportItem struct {
	ID                 ID
	OwnerID            ID
	ImportID           ID
	CanonicalTrackID   ID
	Position           int64
	State              ImportItemState
	DestinationTrackID *ID
	CreatedAt          time.Time
}

type JobKind string

const (
	JobMatch     JobKind = "match"
	JobDeliver   JobKind = "deliver"
	JobReconcile JobKind = "reconcile"
)

type JobState string

const (
	JobReady     JobState = "ready"
	JobLeased    JobState = "leased"
	JobCompleted JobState = "completed"
	JobFailed    JobState = "failed"
)

type ImportJob struct {
	ID            ID
	OwnerID       ID
	ImportID      ID
	ItemID        ID
	Kind          JobKind
	LogicalKey    string
	State         JobState
	AvailableAt   time.Time
	LeaseUntil    *time.Time
	WorkerID      *string
	Generation    int64
	Attempts      int
	MaxAttempts   int
	LastErrorCode string
	CreatedAt     time.Time
}
