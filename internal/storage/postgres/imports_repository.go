package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"musicgetter/internal/domain"
)

type ImportRepository struct{ db DBTX }

func NewImportRepository(db DBTX) *ImportRepository { return &ImportRepository{db} }

const importColumns = `id,owner_id,request_key,source_collection_id,profile_id,source,destination_collection_id,connection_id,state,created_at`

func (r *ImportRepository) Create(ctx context.Context, owner, sourceCollection, destinationCollection domain.ID, requestKey string) (domain.Import, error) {
	row := r.db.QueryRow(ctx, `INSERT INTO musicgetter.imports AS existing
 (owner_id,request_key,source_collection_id,profile_id,source,destination_collection_id,connection_id)
 SELECT $1,$4,s.id,s.profile_id,s.source,d.id,d.connection_id
 FROM musicgetter.source_collections s JOIN musicgetter.destination_collections d ON d.owner_id=s.owner_id
 WHERE s.owner_id=$1 AND s.id=$2 AND d.id=$3
 ON CONFLICT (owner_id,request_key) DO UPDATE SET id=existing.id
 WHERE existing.source_collection_id=EXCLUDED.source_collection_id AND existing.destination_collection_id=EXCLUDED.destination_collection_id
 RETURNING `+importColumns, owner, sourceCollection, destinationCollection, requestKey)
	value, err := scanImport(row)
	if err == domain.ErrNotFound {
		return value, domain.ErrConflict
	}
	return value, err
}
func (r *ImportRepository) Get(ctx context.Context, owner, id domain.ID) (domain.Import, error) {
	return scanImport(r.db.QueryRow(ctx, `SELECT `+importColumns+` FROM musicgetter.imports WHERE owner_id=$1 AND id=$2`, owner, id))
}
func (r *ImportRepository) List(ctx context.Context, owner domain.ID, after *domain.ID, limit int) ([]domain.Import, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT `+importColumns+` FROM musicgetter.imports
 WHERE owner_id=$1 AND ($2::uuid IS NULL OR id>$2) ORDER BY id LIMIT $3`, owner, after, limit)
	if err != nil {
		return nil, repositoryError(err)
	}
	defer rows.Close()
	values := make([]domain.Import, 0, limit)
	for rows.Next() {
		v, err := scanImport(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, repositoryError(rows.Err())
}
func scanImport(row pgx.Row) (domain.Import, error) {
	var v domain.Import
	err := row.Scan(&v.ID, &v.OwnerID, &v.RequestKey, &v.SourceCollectionID, &v.ProfileID, &v.Source, &v.DestinationCollectionID, &v.ConnectionID, &v.State, &v.CreatedAt)
	return v, repositoryError(err)
}
