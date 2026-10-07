package postgres

import (
	"context"
	"time"

	"musicgetter/internal/domain"
)

const leasePredicate = `owner_id=$1 AND id=$2 AND generation=$3 AND worker_id=$4 AND state='leased' AND lease_until>clock_timestamp()`

func (r *JobRepository) Renew(ctx context.Context, j domain.ImportJob, lease time.Duration) (domain.ImportJob, error) {
	if lease < time.Millisecond || lease > time.Hour {
		return domain.ImportJob{}, domain.ErrInvalid
	}
	next, err := scanJob(r.db.QueryRow(ctx, `UPDATE musicgetter.import_jobs
 SET lease_until=clock_timestamp()+$5*interval '1 millisecond' WHERE `+leasePredicate+` RETURNING `+jobColumns,
		j.OwnerID, j.ID, j.Generation, j.WorkerID, lease.Milliseconds()))
	if err == domain.ErrNotFound {
		return next, domain.ErrLeaseLost
	}
	return next, err
}

func (r *JobRepository) Complete(ctx context.Context, j domain.ImportJob) error {
	tag, err := r.db.Exec(ctx, `UPDATE musicgetter.import_jobs SET state='completed',worker_id=NULL,lease_until=NULL WHERE `+leasePredicate,
		j.OwnerID, j.ID, j.Generation, j.WorkerID)
	if err != nil {
		return repositoryError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}
	return nil
}

func (r *JobRepository) Retry(ctx context.Context, j domain.ImportJob, available time.Time, code string) error {
	tag, err := r.db.Exec(ctx, `UPDATE musicgetter.import_jobs SET state=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'ready' END,
 available_at=$5,last_error_code=$6,worker_id=NULL,lease_until=NULL WHERE `+leasePredicate,
		j.OwnerID, j.ID, j.Generation, j.WorkerID, available, code)
	if err != nil {
		return repositoryError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}
	return nil
}

func (r *JobRepository) Fail(ctx context.Context, j domain.ImportJob, code string) error {
	tag, err := r.db.Exec(ctx, `UPDATE musicgetter.import_jobs SET state='failed',last_error_code=$5,worker_id=NULL,lease_until=NULL WHERE `+leasePredicate,
		j.OwnerID, j.ID, j.Generation, j.WorkerID, code)
	if err != nil {
		return repositoryError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}
	return nil
}

// RequeueExpired is bounded. Reclaimed delivery jobs MUST reconcile persisted
// membership intent before retrying a remote effect; this repository does no sends.
func (r *JobRepository) RequeueExpired(ctx context.Context, limit int) (int64, error) {
	if err := pageLimit(limit); err != nil {
		return 0, err
	}
	tag, err := r.db.Exec(ctx, `WITH expired AS (
 SELECT j.id,i.state AS import_state FROM musicgetter.import_jobs j JOIN musicgetter.imports i ON i.id=j.import_id
 WHERE j.state='leased' AND j.lease_until<=clock_timestamp()
 ORDER BY j.lease_until,j.id FOR UPDATE OF j SKIP LOCKED LIMIT $1
 ) UPDATE musicgetter.import_jobs j SET state=CASE WHEN j.attempts>=j.max_attempts OR (j.kind<>'reconcile' AND expired.import_state NOT IN ('queued','processing')) THEN 'failed' ELSE 'ready' END,
 available_at=clock_timestamp(),worker_id=NULL,lease_until=NULL,generation=j.generation+1,
 last_error_code=CASE WHEN expired.import_state='cancelled' THEN 'import_cancelled' ELSE 'lease_expired' END
 FROM expired WHERE j.id=expired.id`, limit)
	return tag.RowsAffected(), repositoryError(err)
}
