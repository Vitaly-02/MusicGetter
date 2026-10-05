package postgres

import (
	"context"

	"musicgetter/internal/domain"
)

type DestinationCollectionRepository struct{ db DBTX }

func NewDestinationCollectionRepository(db DBTX) *DestinationCollectionRepository {
	return &DestinationCollectionRepository{db}
}

func (r *DestinationCollectionRepository) EnsureConnection(ctx context.Context, c domain.DestinationConnection) (domain.DestinationConnection, error) {
	err := r.db.QueryRow(ctx, `INSERT INTO musicgetter.destination_connections(owner_id,adapter,account_key)
 VALUES ($1,$2,$3) ON CONFLICT (owner_id,adapter,account_key) DO UPDATE SET account_key=EXCLUDED.account_key
 RETURNING id,owner_id,adapter,account_key,created_at`, c.OwnerID, c.Adapter, c.AccountKey).
		Scan(&c.ID, &c.OwnerID, &c.Adapter, &c.AccountKey, &c.CreatedAt)
	return c, repositoryError(err)
}

func (r *DestinationCollectionRepository) Ensure(ctx context.Context, c domain.DestinationCollection) (domain.DestinationCollection, error) {
	err := r.db.QueryRow(ctx, `INSERT INTO musicgetter.destination_collections AS existing
 (owner_id,connection_id,external_key,kind,title) VALUES ($1,$2,$3,$4,$5)
 ON CONFLICT (connection_id,external_key) DO UPDATE SET title=EXCLUDED.title
 WHERE existing.owner_id=EXCLUDED.owner_id AND existing.kind=EXCLUDED.kind
 RETURNING id,owner_id,connection_id,external_key,kind,title,created_at`, c.OwnerID, c.ConnectionID, c.ExternalKey, c.Kind, c.Title).
		Scan(&c.ID, &c.OwnerID, &c.ConnectionID, &c.ExternalKey, &c.Kind, &c.Title, &c.CreatedAt)
	return c, conflictError(err)
}

func (r *DestinationCollectionRepository) Get(ctx context.Context, owner, id domain.ID) (domain.DestinationCollection, error) {
	var c domain.DestinationCollection
	err := r.db.QueryRow(ctx, `SELECT id,owner_id,connection_id,external_key,kind,title,created_at
 FROM musicgetter.destination_collections WHERE owner_id=$1 AND id=$2`, owner, id).
		Scan(&c.ID, &c.OwnerID, &c.ConnectionID, &c.ExternalKey, &c.Kind, &c.Title, &c.CreatedAt)
	return c, repositoryError(err)
}
