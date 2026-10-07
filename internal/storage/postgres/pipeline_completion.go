package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	"musicgetter/internal/domain"
	"time"
)

func (r *PipelineRepository) Finish(ctx context.Context, j domain.ImportJob, state domain.ImportItemState, code string) error {
	switch state {
	case domain.ItemAdded, domain.ItemAlreadyPresent, domain.ItemFailed, domain.ItemAmbiguous, domain.ItemNotFound:
	default:
		return domain.ErrInvalid
	}
	return r.leased(ctx, j, func(tx pgx.Tx, i domain.Import) error {
		if state == domain.ItemAdded || state == domain.ItemAlreadyPresent {
			tag, err := tx.Exec(ctx, `UPDATE musicgetter.destination_memberships m SET state='applied',updated_at=clock_timestamp() FROM musicgetter.import_items t WHERE t.id=$1 AND m.owner_id=$2 AND m.collection_id=$3 AND m.destination_track_id=t.destination_track_id`, j.ItemID, j.OwnerID, i.DestinationCollectionID)
			if err != nil {
				return repositoryError(err)
			}
			if tag.RowsAffected() != 1 {
				return domain.ErrConflict
			}
		} else if i.State == domain.ImportCancelled {
			state = domain.ItemFailed
			code = "import_cancelled"
		}
		_, err := tx.Exec(ctx, `UPDATE musicgetter.import_items SET state=$2,error_code=$3 WHERE id=$1 AND owner_id=$4`, j.ItemID, state, code, j.OwnerID)
		if err != nil {
			return repositoryError(err)
		}
		if err = NewJobRepository(tx).Complete(ctx, j); err != nil {
			return err
		}
		return finalizeImport(ctx, tx, i.ID)
	})
}

func (r *PipelineRepository) Reschedule(ctx context.Context, j domain.ImportJob, at time.Time, code string, permanent bool) error {
	return r.leased(ctx, j, func(tx pgx.Tx, i domain.Import) error {
		if i.State == domain.ImportCancelled {
			permanent = true
			code = "import_cancelled"
		}
		if permanent || j.Attempts >= j.MaxAttempts {
			if err := NewJobRepository(tx).Fail(ctx, j, code); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE musicgetter.import_items SET state='failed',error_code=$2 WHERE id=$1 AND state IN ('pending','searching','matched')`, j.ItemID, code)
			if err != nil {
				return repositoryError(err)
			}
			return finalizeImport(ctx, tx, i.ID)
		}
		return NewJobRepository(tx).Retry(ctx, j, at, code)
	})
}

// Parent row is locked by every caller: last concurrent completion observes all
// preceding commits. EXISTS uses the (import_id,state,id) index, not full counts.
func finalizeImport(ctx context.Context, tx pgx.Tx, id domain.ID) error {
	_, err := tx.Exec(ctx, `UPDATE musicgetter.imports i SET state=CASE WHEN EXISTS(SELECT 1 FROM musicgetter.import_items t WHERE t.import_id=i.id AND t.state IN ('failed','ambiguous','not_found')) THEN 'completed_with_errors' ELSE 'completed' END
 WHERE i.id=$1 AND i.state IN ('queued','processing') AND NOT EXISTS(SELECT 1 FROM musicgetter.import_items t WHERE t.import_id=i.id AND t.state IN ('pending','searching','matched'))`, id)
	return repositoryError(err)
}

// Repair closes items whose last lease expired at its retry budget, including
// jobs failed by cancellation. It never discards unknown membership intents.
func (r *PipelineRepository) Repair(ctx context.Context, limit int) error {
	if err := pageLimit(limit); err != nil {
		return err
	}
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT j.owner_id,j.import_id FROM musicgetter.import_jobs j JOIN musicgetter.import_items t ON t.id=j.item_id WHERE j.state='failed' AND t.state IN ('pending','searching','matched') LIMIT $1`, limit)
	if err != nil {
		return repositoryError(err)
	}
	type key struct{ owner, id domain.ID }
	keys := make([]key, 0, limit)
	for rows.Next() {
		var k key
		if err = rows.Scan(&k.owner, &k.id); err != nil {
			rows.Close()
			return repositoryError(err)
		}
		keys = append(keys, k)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return repositoryError(err)
	}
	for _, k := range keys {
		if err = r.repairOne(ctx, k.owner, k.id, limit); err != nil {
			return err
		}
	}
	return nil
}
func (r *PipelineRepository) repairOne(ctx context.Context, owner, id domain.ID, limit int) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return repositoryError(err)
	}
	defer tx.Rollback(ctx)
	var locked domain.ID
	if err = tx.QueryRow(ctx, `SELECT id FROM musicgetter.imports WHERE owner_id=$1 AND id=$2 FOR UPDATE`, owner, id).Scan(&locked); err != nil {
		return repositoryError(err)
	}
	_, err = tx.Exec(ctx, `WITH picked AS (SELECT t.id,j.last_error_code FROM musicgetter.import_items t JOIN musicgetter.import_jobs j ON j.item_id=t.id WHERE t.import_id=$1 AND t.state IN ('pending','searching','matched') AND j.state='failed' LIMIT $2) UPDATE musicgetter.import_items t SET state='failed',error_code=p.last_error_code FROM picked p WHERE t.id=p.id`, id, limit)
	if err != nil {
		return repositoryError(err)
	}
	if err = finalizeImport(ctx, tx, id); err != nil {
		return err
	}
	return repositoryError(tx.Commit(ctx))
}
