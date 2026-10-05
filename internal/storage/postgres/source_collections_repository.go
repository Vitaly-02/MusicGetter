package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"musicgetter/internal/domain"
)

type SourceRepository struct{ db DBTX }

func NewSourceRepository(db DBTX) *SourceRepository { return &SourceRepository{db} }

func (r *SourceRepository) EnsureProfile(ctx context.Context, p domain.SourceProfile) (domain.SourceProfile, error) {
	err := r.db.QueryRow(ctx, `INSERT INTO musicgetter.source_profiles(owner_id,source,profile_key,label)
 VALUES ($1,$2,$3,$4) ON CONFLICT (owner_id,source,profile_key) DO UPDATE SET label=EXCLUDED.label
 RETURNING id,owner_id,source,profile_key,label,created_at`, p.OwnerID, p.Source, p.ProfileKey, p.Label).
		Scan(&p.ID, &p.OwnerID, &p.Source, &p.ProfileKey, &p.Label, &p.CreatedAt)
	return p, repositoryError(err)
}

func (r *SourceRepository) EnsureCollection(ctx context.Context, c domain.SourceCollection) (domain.SourceCollection, error) {
	err := r.db.QueryRow(ctx, `INSERT INTO musicgetter.source_collections AS existing
 (owner_id,profile_id,source,collection_key,provisional,kind,title) VALUES ($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT (profile_id,collection_key) DO UPDATE SET title=EXCLUDED.title
 WHERE existing.owner_id=EXCLUDED.owner_id AND existing.source=EXCLUDED.source
 AND existing.kind=EXCLUDED.kind AND existing.provisional=EXCLUDED.provisional
 RETURNING id,owner_id,profile_id,source,collection_key,provisional,kind,title,created_at`,
		c.OwnerID, c.ProfileID, c.Source, c.CollectionKey, c.Provisional, c.Kind, c.Title).
		Scan(&c.ID, &c.OwnerID, &c.ProfileID, &c.Source, &c.CollectionKey, &c.Provisional, &c.Kind, &c.Title, &c.CreatedAt)
	return c, conflictError(err)
}

func (r *SourceRepository) GetCollection(ctx context.Context, owner, id domain.ID) (domain.SourceCollection, error) {
	return scanSourceCollection(r.db.QueryRow(ctx, `SELECT id,owner_id,profile_id,source,collection_key,provisional,kind,title,created_at
 FROM musicgetter.source_collections WHERE owner_id=$1 AND id=$2`, owner, id))
}

func scanSourceCollection(row pgx.Row) (domain.SourceCollection, error) {
	var c domain.SourceCollection
	err := row.Scan(&c.ID, &c.OwnerID, &c.ProfileID, &c.Source, &c.CollectionKey, &c.Provisional, &c.Kind, &c.Title, &c.CreatedAt)
	return c, repositoryError(err)
}
