package importer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"musicgetter/internal/domain"
	"time"
)

const (
	MaxChunkTracks  = 200
	MaxPayloadBytes = 512 * 1024
	MaxObservations = 100000
	MaxChunks       = 10000
)

type SourceInput struct {
	Service       domain.Source               `json:"service"`
	ProfileKey    string                      `json:"profile_key"`
	CollectionKey string                      `json:"collection_key"`
	Kind          domain.SourceCollectionKind `json:"kind"`
	Title         string                      `json:"title"`
	Provisional   bool                        `json:"provisional"`
}
type CreateRequest struct {
	ClientRequestID         string      `json:"client_request_id"`
	Source                  SourceInput `json:"source"`
	DestinationCollectionID domain.ID   `json:"destination_collection_id"`
}
type TrackInput struct {
	Title          string   `json:"title"`
	Artists        []string `json:"artists"`
	Album          string   `json:"album,omitempty"`
	DurationMS     *int64   `json:"duration_ms,omitempty"`
	Version        string   `json:"version,omitempty"`
	SourceTrackKey *string  `json:"source_track_key,omitempty"`
	SourceURL      *string  `json:"source_url,omitempty"`
	Position       *int64   `json:"position"`
}

func (t TrackInput) Metadata() domain.TrackMetadata {
	return domain.TrackMetadata{Title: t.Title, Artists: t.Artists, Album: t.Album, DurationMS: t.DurationMS, Version: t.Version}
}

type ChunkRequest struct {
	SchemaVersion  int          `json:"schema_version"`
	IdempotencyKey string       `json:"idempotency_key"`
	Sequence       *int64       `json:"sequence"`
	Tracks         []TrackInput `json:"tracks"`
}
type CompleteRequest struct {
	LastSequence  *int64 `json:"last_sequence"`
	ObservedCount *int64 `json:"observed_count"`
	Completeness  string `json:"completeness"`
	Reason        string `json:"reason"`
}
type Created struct {
	ID     domain.ID `json:"id"`
	Replay bool      `json:"replay"`
}
type ChunkReceipt struct {
	Sequence       int64  `json:"sequence"`
	IdempotencyKey string `json:"idempotency_key"`
	Received       int    `json:"received"`
	Added          int    `json:"added"`
	Replay         bool   `json:"replay"`
}
type UploadView struct {
	ID                   domain.ID           `json:"id"`
	State                domain.ImportState  `json:"state"`
	CaptureState         domain.CaptureState `json:"capture_state"`
	ReceivedChunks       int64               `json:"received_chunks"`
	ReceivedObservations int64               `json:"received_observations"`
	ContiguousThrough    int64               `json:"contiguous_through"`
	TotalTracks          int64               `json:"total_tracks"`
	Added                int64               `json:"added"`
	AlreadyPresent       int64               `json:"already_present"`
	Failed               int64               `json:"failed"`
	NeedsReview          int64               `json:"needs_review"`
	Cancelled            int64               `json:"cancelled"`
	CreatedAt            time.Time           `json:"created_at"`
}
type DestinationView struct {
	ID           domain.ID                        `json:"id"`
	ConnectionID domain.ID                        `json:"connection_id"`
	Adapter      string                           `json:"adapter"`
	Kind         domain.DestinationCollectionKind `json:"kind"`
	Title        string                           `json:"title"`
}
type UploadStore interface {
	CreateUpload(context.Context, domain.ID, CreateRequest) (Created, error)
	AppendChunk(context.Context, domain.ID, domain.ID, ChunkRequest) (ChunkReceipt, error)
	CompleteUpload(context.Context, domain.ID, domain.ID, CompleteRequest) error
	CancelUpload(context.Context, domain.ID, domain.ID) error
	GetUpload(context.Context, domain.ID, domain.ID) (UploadView, error)
}

// Digest is computed by the server from validated typed fields, not client bytes.
// Field order/JSON whitespace do not matter; track array order and metadata do.
func Digest(value any) []byte {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return sum[:]
}
