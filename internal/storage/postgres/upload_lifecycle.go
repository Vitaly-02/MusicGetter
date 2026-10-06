package postgres

import (
	"bytes"
	"context"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
)

func (r *UploadRepository) CompleteUpload(ctx context.Context, owner, id domain.ID, c importer.CompleteRequest) error {
	if err := c.Validate(); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return repositoryError(err)
	}
	defer tx.Rollback(ctx)
	u, err := lockUpload(ctx, tx, owner, id)
	if err != nil {
		return err
	}
	digest := importer.Digest(c)
	if u.completeDigest != nil {
		if bytes.Equal(u.completeDigest, digest) {
			return repositoryError(tx.Commit(ctx))
		}
		return domain.ErrConflict
	}
	if u.capture != domain.CaptureCollecting || u.state != domain.ImportCollecting || u.chunks != *c.LastSequence+1 || u.through != *c.LastSequence || u.observations != *c.ObservedCount {
		return domain.ErrConflict
	}
	state := domain.CaptureSealedPartial
	if c.Completeness == "complete" {
		state = domain.CaptureSealedComplete
	}
	_, err = tx.Exec(ctx, `UPDATE musicgetter.import_uploads SET capture_state=$2,complete_digest=$3,last_sequence=$4,completion_reason=$5 WHERE import_id=$1`, id, state, digest, *c.LastSequence, c.Reason)
	if err != nil {
		return repositoryError(err)
	}
	// Set-based enqueue keeps memory bounded even for 100,000 observations.
	_, err = tx.Exec(ctx, `INSERT INTO musicgetter.import_jobs(owner_id,import_id,item_id,kind,logical_key) SELECT owner_id,import_id,id,'match',id::text FROM musicgetter.import_items WHERE import_id=$1 ON CONFLICT(item_id,kind) DO NOTHING`, id)
	if err != nil {
		return repositoryError(err)
	}
	next := domain.ImportQueued
	if u.observations == 0 {
		next = domain.ImportCompleted
	}
	_, err = tx.Exec(ctx, `UPDATE musicgetter.imports SET state=$2 WHERE id=$1`, id, next)
	if err != nil {
		return repositoryError(err)
	}
	return repositoryError(tx.Commit(ctx))
}
func (r *UploadRepository) CancelUpload(ctx context.Context, owner, id domain.ID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return repositoryError(err)
	}
	defer tx.Rollback(ctx)
	u, err := lockUpload(ctx, tx, owner, id)
	if err != nil {
		return err
	}
	if u.state == domain.ImportCancelled {
		return repositoryError(tx.Commit(ctx))
	}
	if u.state != domain.ImportCollecting && u.state != domain.ImportQueued && u.state != domain.ImportRunning && u.state != domain.ImportNeedsAttention {
		return domain.ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE musicgetter.imports SET state='cancelled' WHERE id=$1`, id)
	if err != nil {
		return repositoryError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE musicgetter.import_uploads SET capture_state='aborted' WHERE import_id=$1 AND capture_state='collecting'`, id)
	if err != nil {
		return repositoryError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE musicgetter.import_items SET state='cancelled' WHERE import_id=$1 AND state IN ('pending','searching','needs_review','matched')`, id)
	if err != nil {
		return repositoryError(err)
	}
	// Leased/uncertain external operations require worker checks/reconciliation.
	_, err = tx.Exec(ctx, `UPDATE musicgetter.import_jobs SET state='failed',last_error_code='import_cancelled' WHERE import_id=$1 AND state='ready' AND kind<>'reconcile'`, id)
	if err != nil {
		return repositoryError(err)
	}
	return repositoryError(tx.Commit(ctx))
}
