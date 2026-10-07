package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
)

type JobRepository struct{ db DBTX }

var _ importer.JobQueue = (*JobRepository)(nil)

func NewJobRepository(db DBTX) *JobRepository { return &JobRepository{db} }

const jobColumns = `id,owner_id,import_id,item_id,kind,logical_key,state,available_at,lease_until,worker_id,generation,attempts,max_attempts,last_error_code,created_at`

func (r *JobRepository) Enqueue(ctx context.Context, j domain.ImportJob) (domain.ImportJob, error) {
	if j.MaxAttempts == 0 {
		j.MaxAttempts = 8
	}
	if j.AvailableAt.IsZero() {
		j.AvailableAt = time.Now().UTC()
	}
	out, err := scanJob(r.db.QueryRow(ctx, `INSERT INTO musicgetter.import_jobs AS existing
 (owner_id,import_id,item_id,kind,logical_key,available_at,max_attempts) VALUES ($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT (item_id,kind) DO UPDATE SET id=existing.id
 WHERE existing.owner_id=EXCLUDED.owner_id AND existing.import_id=EXCLUDED.import_id
 AND existing.logical_key=EXCLUDED.logical_key AND existing.max_attempts=EXCLUDED.max_attempts
 RETURNING `+jobColumns, j.OwnerID, j.ImportID, j.ItemID, j.Kind, j.LogicalKey, j.AvailableAt, j.MaxAttempts))
	if err == domain.ErrNotFound {
		return out, domain.ErrConflict
	}
	return out, err
}

func (r *JobRepository) Get(ctx context.Context, owner, id domain.ID) (domain.ImportJob, error) {
	return scanJob(r.db.QueryRow(ctx, `SELECT `+jobColumns+` FROM musicgetter.import_jobs WHERE owner_id=$1 AND id=$2`, owner, id))
}

// Claim is a privileged worker operation across owners. Use a pool for a short
// autocommit claim. A caller supplying a Tx must commit before external I/O.
func (r *JobRepository) Claim(ctx context.Context, worker string, limit int, lease time.Duration) ([]domain.ImportJob, error) {
	if pageLimit(limit) != nil || lease < time.Millisecond || lease > time.Hour || len(worker) == 0 || len(worker) > 128 {
		return nil, domain.ErrInvalid
	}
	rows, err := r.db.Query(ctx, `WITH picked AS (
 SELECT j.id FROM musicgetter.import_jobs j JOIN musicgetter.imports i ON i.id=j.import_id
 WHERE j.state='ready' AND j.available_at<=clock_timestamp() AND j.attempts<j.max_attempts
 AND (i.state IN ('queued','processing') OR j.kind='reconcile')
 ORDER BY j.available_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT $1
 ) UPDATE musicgetter.import_jobs j SET state='leased',worker_id=$2,
 lease_until=clock_timestamp()+$3*interval '1 millisecond',generation=j.generation+1,attempts=j.attempts+1
 FROM picked WHERE j.id=picked.id RETURNING `+qualifiedJobColumns, limit, worker, lease.Milliseconds())
	if err != nil {
		return nil, repositoryError(err)
	}
	defer rows.Close()
	jobs := make([]domain.ImportJob, 0, limit)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, repositoryError(rows.Err())
}

const qualifiedJobColumns = `j.id,j.owner_id,j.import_id,j.item_id,j.kind,j.logical_key,j.state,j.available_at,j.lease_until,j.worker_id,j.generation,j.attempts,j.max_attempts,j.last_error_code,j.created_at`

func scanJob(row pgx.Row) (domain.ImportJob, error) {
	var j domain.ImportJob
	err := row.Scan(&j.ID, &j.OwnerID, &j.ImportID, &j.ItemID, &j.Kind, &j.LogicalKey, &j.State, &j.AvailableAt, &j.LeaseUntil, &j.WorkerID, &j.Generation, &j.Attempts, &j.MaxAttempts, &j.LastErrorCode, &j.CreatedAt)
	return j, repositoryError(err)
}
