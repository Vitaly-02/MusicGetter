package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"musicgetter/internal/domain"
)

const itemColumns = `id,owner_id,import_id,canonical_track_id,position,state,destination_track_id,created_at,error_code`

// First occurrence wins. A replay does not change position, state or selected track.
func (r *ImportRepository) AddItem(ctx context.Context, owner, importID, trackID domain.ID, position int64) (domain.ImportItem, error) {
	return scanItem(r.db.QueryRow(ctx, `INSERT INTO musicgetter.import_items AS existing
 (owner_id,import_id,canonical_track_id,profile_id,source,connection_id,position)
 SELECT owner_id,id,$3,profile_id,source,connection_id,$4 FROM musicgetter.imports WHERE owner_id=$1 AND id=$2
 ON CONFLICT (import_id,canonical_track_id) DO UPDATE SET id=existing.id
 RETURNING `+itemColumns, owner, importID, trackID, position))
}

func (r *ImportRepository) ListItems(ctx context.Context, owner, importID domain.ID, after *domain.ID, limit int) ([]domain.ImportItem, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT `+itemColumns+` FROM musicgetter.import_items
 WHERE owner_id=$1 AND import_id=$2 AND ($3::uuid IS NULL OR id>$3) ORDER BY id LIMIT $4`, owner, importID, after, limit)
	if err != nil {
		return nil, repositoryError(err)
	}
	defer rows.Close()
	values := make([]domain.ImportItem, 0, limit)
	for rows.Next() {
		v, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, repositoryError(rows.Err())
}
func scanItem(row pgx.Row) (domain.ImportItem, error) {
	var v domain.ImportItem
	err := row.Scan(&v.ID, &v.OwnerID, &v.ImportID, &v.CanonicalTrackID, &v.Position, &v.State, &v.DestinationTrackID, &v.CreatedAt, &v.ErrorCode)
	return v, repositoryError(err)
}
