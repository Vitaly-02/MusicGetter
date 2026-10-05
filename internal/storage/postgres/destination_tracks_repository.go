package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"musicgetter/internal/domain"
)

type DestinationTrackRepository struct{ db DBTX }

func NewDestinationTrackRepository(db DBTX) *DestinationTrackRepository {
	return &DestinationTrackRepository{db}
}

const destinationTrackColumns = `id,owner_id,connection_id,external_key,title,artists,album,duration_ms,edition,created_at`

func (r *DestinationTrackRepository) Ensure(ctx context.Context, t domain.DestinationTrack) (domain.DestinationTrack, error) {
	if err := domain.ValidateMetadata(t.Metadata); err != nil {
		return domain.DestinationTrack{}, err
	}
	return scanDestinationTrack(r.db.QueryRow(ctx, `INSERT INTO musicgetter.destination_tracks AS existing
 (owner_id,connection_id,external_key,title,artists,album,duration_ms,edition) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
 ON CONFLICT (connection_id,external_key) DO UPDATE SET id=existing.id WHERE existing.owner_id=EXCLUDED.owner_id
 RETURNING `+destinationTrackColumns, t.OwnerID, t.ConnectionID, t.ExternalKey, t.Metadata.Title, t.Metadata.Artists, t.Metadata.Album, t.Metadata.DurationMS, t.Metadata.Version))
}

func (r *DestinationTrackRepository) Get(ctx context.Context, owner, id domain.ID) (domain.DestinationTrack, error) {
	return scanDestinationTrack(r.db.QueryRow(ctx, `SELECT `+destinationTrackColumns+` FROM musicgetter.destination_tracks WHERE owner_id=$1 AND id=$2`, owner, id))
}

func scanDestinationTrack(row pgx.Row) (domain.DestinationTrack, error) {
	var t domain.DestinationTrack
	err := row.Scan(&t.ID, &t.OwnerID, &t.ConnectionID, &t.ExternalKey, &t.Metadata.Title, &t.Metadata.Artists, &t.Metadata.Album, &t.Metadata.DurationMS, &t.Metadata.Version, &t.CreatedAt)
	return t, repositoryError(err)
}
