package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"musicgetter/internal/domain"
)

type MappingRepository struct{ db DBTX }

func NewMappingRepository(db DBTX) *MappingRepository { return &MappingRepository{db} }

const mappingColumns = `id,owner_id,canonical_track_id,connection_id,destination_track_id,origin,policy_version,created_at`

// A different match is a conflict, never a silent overwrite of an accepted decision.
func (r *MappingRepository) Save(ctx context.Context, m domain.TrackMapping) (domain.TrackMapping, error) {
	row := r.db.QueryRow(ctx, `INSERT INTO musicgetter.track_mappings AS existing
 (owner_id,canonical_track_id,connection_id,destination_track_id,origin,policy_version) VALUES ($1,$2,$3,$4,$5,$6)
 ON CONFLICT (canonical_track_id,connection_id) DO UPDATE SET id=existing.id
 WHERE existing.owner_id=EXCLUDED.owner_id AND existing.destination_track_id=EXCLUDED.destination_track_id
 AND existing.origin=EXCLUDED.origin AND existing.policy_version=EXCLUDED.policy_version RETURNING `+mappingColumns,
		m.OwnerID, m.CanonicalTrackID, m.ConnectionID, m.DestinationTrackID, m.Origin, m.PolicyVersion)
	out, err := scanMapping(row)
	if err == domain.ErrNotFound {
		return out, domain.ErrConflict
	}
	return out, err
}

func (r *MappingRepository) Get(ctx context.Context, owner, track, connection domain.ID) (domain.TrackMapping, error) {
	return scanMapping(r.db.QueryRow(ctx, `SELECT `+mappingColumns+` FROM musicgetter.track_mappings
 WHERE owner_id=$1 AND canonical_track_id=$2 AND connection_id=$3`, owner, track, connection))
}
func scanMapping(row pgx.Row) (domain.TrackMapping, error) {
	var m domain.TrackMapping
	err := row.Scan(&m.ID, &m.OwnerID, &m.CanonicalTrackID, &m.ConnectionID, &m.DestinationTrackID, &m.Origin, &m.PolicyVersion, &m.CreatedAt)
	return m, repositoryError(err)
}
