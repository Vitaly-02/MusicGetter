package domain

import "time"

type User struct {
	ID             ID
	TelegramUserID int64
	CreatedAt      time.Time
}

// Pairing stores only SHA-256 digests, never a plaintext code or verifier.
type ExtensionPairing struct {
	ID            ID
	OwnerID       *ID
	CodeHash      []byte
	ChallengeHash []byte
	ExpiresAt     time.Time
	ConsumedAt    *time.Time
	CreatedAt     time.Time
}

type SourceProfile struct {
	ID         ID
	OwnerID    ID
	Source     Source
	ProfileKey string
	Label      string
	CreatedAt  time.Time
}

type SourceCollectionKind string

const (
	SourceFavorites SourceCollectionKind = "favorites"
	SourcePlaylist  SourceCollectionKind = "playlist"
	SourceAlbum     SourceCollectionKind = "album"
	SourceSelection SourceCollectionKind = "selection"
)

type SourceCollection struct {
	ID            ID
	OwnerID       ID
	ProfileID     ID
	Source        Source
	CollectionKey string // stable rendered key or client-generated collection UUID
	Provisional   bool
	Kind          SourceCollectionKind
	Title         string
	CreatedAt     time.Time
}

// DestinationConnection identifies an adapter/account, not its credentials.
type DestinationConnection struct {
	ID         ID
	OwnerID    ID
	Adapter    string
	AccountKey string
	CreatedAt  time.Time
}

type DestinationCollection struct {
	ID           ID
	OwnerID      ID
	ConnectionID ID
	ExternalKey  string
	Kind         DestinationCollectionKind
	Title        string
	CreatedAt    time.Time
}
