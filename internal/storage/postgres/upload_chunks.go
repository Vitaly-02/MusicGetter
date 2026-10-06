package postgres

import (
	"bytes"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
)

func (r *UploadRepository) AppendChunk(ctx context.Context, owner, id domain.ID, c importer.ChunkRequest) (importer.ChunkReceipt, error) {
	var receipt importer.ChunkReceipt
	if err := c.Validate(); err != nil {
		return receipt, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return receipt, repositoryError(err)
	}
	defer tx.Rollback(ctx)
	u, err := lockUpload(ctx, tx, owner, id)
	if err != nil {
		return receipt, err
	}
	var digest []byte
	err = tx.QueryRow(ctx, `SELECT sequence,idempotency_key,received,added,payload_digest FROM musicgetter.import_chunks WHERE import_id=$1 AND (sequence=$2 OR idempotency_key=$3) ORDER BY sequence LIMIT 1`, id, *c.Sequence, c.IdempotencyKey).Scan(&receipt.Sequence, &receipt.IdempotencyKey, &receipt.Received, &receipt.Added, &digest)
	if err == nil {
		if receipt.Sequence != *c.Sequence || receipt.IdempotencyKey != c.IdempotencyKey || !bytes.Equal(digest, importer.Digest(c)) {
			return receipt, domain.ErrConflict
		}
		receipt.Replay = true
		return receipt, repositoryError(tx.Commit(ctx))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return receipt, repositoryError(err)
	}
	if u.capture != domain.CaptureCollecting || u.state != domain.ImportCollecting {
		return receipt, domain.ErrConflict
	}
	if u.observations+int64(len(c.Tracks)) > importer.MaxObservations {
		return receipt, domain.ErrInvalid
	}
	receipt = importer.ChunkReceipt{Sequence: *c.Sequence, IdempotencyKey: c.IdempotencyKey, Received: len(c.Tracks)}
	tracks := NewCanonicalTrackRepository(tx)
	for _, track := range c.Tracks {
		canonical, err := tracks.Ensure(ctx, domain.CanonicalTrackInput{OwnerID: owner, ProfileID: u.profile, Source: u.source, Metadata: track.Metadata(), SourceTrackKey: track.SourceTrackKey, SourceURL: track.SourceURL})
		if err != nil {
			return receipt, err
		}
		result, err := tx.Exec(ctx, `INSERT INTO musicgetter.import_items(owner_id,import_id,canonical_track_id,profile_id,source,connection_id,position) SELECT owner_id,id,$3,profile_id,source,connection_id,$4 FROM musicgetter.imports WHERE owner_id=$1 AND id=$2 ON CONFLICT(import_id,canonical_track_id) DO NOTHING`, owner, id, canonical.ID, *track.Position)
		if err != nil {
			return receipt, repositoryError(err)
		}
		receipt.Added += int(result.RowsAffected())
	}
	_, err = tx.Exec(ctx, `INSERT INTO musicgetter.import_chunks(import_id,owner_id,sequence,idempotency_key,payload_digest,received,added) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, owner, receipt.Sequence, receipt.IdempotencyKey, importer.Digest(c), receipt.Received, receipt.Added)
	if err != nil {
		return receipt, repositoryError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE musicgetter.import_uploads u SET received_chunks=received_chunks+1,received_observations=received_observations+$2,contiguous_through=CASE WHEN $3=contiguous_through+1 THEN (SELECT min(c.sequence) FROM musicgetter.import_chunks c WHERE c.import_id=u.import_id AND c.sequence>u.contiguous_through AND NOT EXISTS(SELECT 1 FROM musicgetter.import_chunks n WHERE n.import_id=c.import_id AND n.sequence=c.sequence+1)) ELSE contiguous_through END WHERE import_id=$1`, id, receipt.Received, receipt.Sequence)
	if err != nil {
		return receipt, repositoryError(err)
	}
	return receipt, repositoryError(tx.Commit(ctx))
}
