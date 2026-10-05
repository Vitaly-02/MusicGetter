// Package domain contains transport-independent entities and identity rules.
package domain

import "time"

type ID string
type Source string

const (
	SourceSpotify Source = "spotify"
	SourceYandex  Source = "yandex"
	SourceVK      Source = "vk"
)

type DestinationCollectionKind string

const (
	CollectionFavorites DestinationCollectionKind = "favorites"
	CollectionPlaylist  DestinationCollectionKind = "playlist"
	CollectionAlbum     DestinationCollectionKind = "album"
)

// SourceRef is scoped to a user-owned source profile, never a streaming credential.
// Key is derived from an allowed rendered link, or explicitly marked provisional.
type SourceRef struct {
	ProfileID   ID
	Source      Source
	Key         string
	Provisional bool
}

type TrackMetadata struct {
	Title      string
	Artists    []string
	Album      string
	DurationMS *int64
	Version    string
}

type ObservedTrack struct {
	Ref      SourceRef
	Metadata TrackMetadata
}

// Position is an observation within a capture, not a global track identity.
type CollectionItem struct {
	Track    ObservedTrack
	Position int64
}

type CaptureState string

const (
	CaptureCollecting     CaptureState = "collecting"
	CaptureSealedPartial  CaptureState = "sealed_partial"
	CaptureSealedComplete CaptureState = "sealed_complete"
	CaptureAborted        CaptureState = "aborted"
)

type ImportState string

const (
	ImportQueued              ImportState = "queued"
	ImportRunning             ImportState = "running"
	ImportNeedsAttention      ImportState = "needs_attention"
	ImportCompleted           ImportState = "completed"
	ImportCompletedWithErrors ImportState = "completed_with_errors"
	ImportCancelled           ImportState = "cancelled"
	ImportFailed              ImportState = "failed"
)

type Target struct {
	ConnectionID ID
	ExternalID   string
	Kind         DestinationCollectionKind
}

type ImportProgress struct {
	ImportID       ID
	State          ImportState
	Total          int64
	Pending        int64
	Matched        int64
	NeedsReview    int64
	Added          int64
	AlreadyPresent int64
	Failed         int64
	Skipped        int64
	Cancelled      int64
	UpdatedAt      time.Time
}
