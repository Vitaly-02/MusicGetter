package postgres

import (
	"bytes"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
)

type UploadRepository struct{ pool *pgxpool.Pool }

func NewUploadRepository(pool *pgxpool.Pool) *UploadRepository { return &UploadRepository{pool} }

var _ importer.UploadStore = (*UploadRepository)(nil)

func (r *UploadRepository) CreateUpload(ctx context.Context, owner domain.ID, c importer.CreateRequest) (importer.Created, error) {
	var out importer.Created
	if err := c.Validate(); err != nil {
		return out, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return out, repositoryError(err)
	}
	defer tx.Rollback(ctx)
	// Serialize create retries for this owner; no stream credentials participate.
	if err = lockSessionOwner(ctx, tx, owner); err != nil {
		return out, repositoryError(err)
	}
	var digest []byte
	err = tx.QueryRow(ctx, `SELECT i.id,u.create_digest FROM musicgetter.imports i LEFT JOIN musicgetter.import_uploads u ON u.import_id=i.id WHERE i.owner_id=$1 AND i.request_key=$2`, owner, c.ClientRequestID).Scan(&out.ID, &digest)
	if err == nil {
		if !bytes.Equal(digest, importer.Digest(c)) {
			return out, domain.ErrConflict
		}
		out.Replay = true
		return out, repositoryError(tx.Commit(ctx))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return out, repositoryError(err)
	}
	if _, err = NewDestinationCollectionRepository(tx).Get(ctx, owner, c.DestinationCollectionID); err != nil {
		return out, err
	}
	source := NewSourceRepository(tx)
	var profileID domain.ID
	err = tx.QueryRow(ctx, `INSERT INTO musicgetter.source_profiles AS existing(owner_id,source,profile_key,label) VALUES($1,$2,$3,'') ON CONFLICT(owner_id,source,profile_key) DO UPDATE SET id=existing.id RETURNING id`, owner, c.Source.Service, c.Source.ProfileKey).Scan(&profileID)
	if err != nil {
		return out, repositoryError(err)
	}
	collection, err := source.EnsureCollection(ctx, domain.SourceCollection{OwnerID: owner, ProfileID: profileID, Source: c.Source.Service, CollectionKey: c.Source.CollectionKey, Kind: c.Source.Kind, Title: c.Source.Title, Provisional: c.Source.Provisional})
	if err != nil {
		return out, err
	}
	record, err := NewImportRepository(tx).Create(ctx, owner, collection.ID, c.DestinationCollectionID, c.ClientRequestID)
	if err != nil {
		return out, err
	}
	out.ID = record.ID
	_, err = tx.Exec(ctx, `UPDATE musicgetter.imports SET state='created' WHERE id=$1`, out.ID)
	if err != nil {
		return out, repositoryError(err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO musicgetter.import_uploads(import_id,owner_id,create_digest) VALUES($1,$2,$3)`, out.ID, owner, importer.Digest(c))
	if err != nil {
		return out, repositoryError(err)
	}
	return out, repositoryError(tx.Commit(ctx))
}

type lockedUpload struct {
	profile                       domain.ID
	source                        domain.Source
	state                         domain.ImportState
	capture                       domain.CaptureState
	completeDigest                []byte
	chunks, observations, through int64
}

func lockUpload(ctx context.Context, tx pgx.Tx, owner, id domain.ID) (lockedUpload, error) {
	var u lockedUpload
	err := tx.QueryRow(ctx, `SELECT i.profile_id,i.source,i.state,u.capture_state,u.complete_digest,u.received_chunks,u.received_observations,u.contiguous_through FROM musicgetter.imports i JOIN musicgetter.import_uploads u ON u.import_id=i.id WHERE i.owner_id=$1 AND i.id=$2 FOR UPDATE OF i,u`, owner, id).Scan(&u.profile, &u.source, &u.state, &u.capture, &u.completeDigest, &u.chunks, &u.observations, &u.through)
	return u, repositoryError(err)
}
