package domain

import "time"

type Import struct {
	ResolvedDestinationCollectionID *ID
	ID                              ID
	OwnerID                         ID
	RequestKey                      string
	SourceCollectionID              ID
	ProfileID                       ID
	Source                          Source
	DestinationCollectionID         ID
	ConnectionID                    ID
	State                           ImportState
	CreatedAt                       time.Time
}

type ImportItemState string

const (
	ItemNotFound       ImportItemState = "not_found"
	ItemPending        ImportItemState = "pending"
	ItemSearching      ImportItemState = "searching"
	ItemAmbiguous      ImportItemState = "ambiguous"
	ItemMatched        ImportItemState = "matched"
	ItemAdded          ImportItemState = "added"
	ItemAlreadyPresent ImportItemState = "already_present"
	ItemFailed         ImportItemState = "failed"
)

type ImportItem struct {
	ErrorCode          string
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
