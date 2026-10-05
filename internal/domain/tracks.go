package domain

import "time"

type CanonicalTrackInput struct {
	OwnerID        ID
	ProfileID      ID
	Source         Source
	Metadata       TrackMetadata
	SourceTrackKey *string
	SourceURL      *string
}

// CanonicalTrack is a source observation identity, not an acoustic recording ID.
// Fingerprint is evidence for exact metadata fallback, never a fuzzy match score.
type CanonicalTrack struct {
	ID ID
	CanonicalTrackInput
	NormalizedTitle   string
	NormalizedArtists []string
	Fingerprint       string
	IdentityKey       string
	CreatedAt         time.Time
}

// ID is the local UUID. Adapters use ExternalKey when invoking a destination.
type DestinationTrack struct {
	ID           ID
	OwnerID      ID
	ConnectionID ID
	ExternalKey  string
	Metadata     TrackMetadata
	CreatedAt    time.Time
}

type MappingOrigin string

const (
	MappingManual    MappingOrigin = "manual"
	MappingAutomatic MappingOrigin = "automatic"
)

type TrackMapping struct {
	ID                 ID
	OwnerID            ID
	CanonicalTrackID   ID
	ConnectionID       ID
	DestinationTrackID ID
	Origin             MappingOrigin
	PolicyVersion      string
	CreatedAt          time.Time
}

type MembershipState string

const (
	MembershipReserved MembershipState = "reserved"
	MembershipUnknown  MembershipState = "unknown"
	MembershipApplied  MembershipState = "applied"
)

// A reserved membership is intent, not confirmation of a remote side effect.
type DestinationMembership struct {
	ID                 ID
	OwnerID            ID
	ConnectionID       ID
	CollectionID       ID
	DestinationTrackID ID
	OperationKey       ID
	State              MembershipState
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
