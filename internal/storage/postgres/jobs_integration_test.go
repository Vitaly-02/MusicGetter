//go:build integration

package postgres_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"musicgetter/internal/domain"
	"musicgetter/internal/storage/postgres"
)

func TestConcurrentJobClaimsAndLeaseFencing(t *testing.T) {
	f := newFixture(t)
	repo := postgres.NewJobRepository(f.pool)
	record := f.importRecord(t, "queue")
	const jobs = 12
	for i := range jobs {
		item := f.item(t, record, fmt.Sprintf("Song %d", i))
		f.enqueue(t, item, 3)
	}
	results := make([][]domain.ImportJob, 4)
	errs := make([]error, 4)
	var group sync.WaitGroup
	for i := range 4 {
		group.Go(func() { results[i], errs[i] = repo.Claim(f.ctx, fmt.Sprintf("worker-%d", i), 3, time.Minute) })
	}
	group.Wait()
	seen := map[domain.ID]bool{}
	var first domain.ImportJob
	for i := range 4 {
		requireOK(t, errs[i])
		for _, job := range results[i] {
			if seen[job.ID] {
				t.Fatal("job leased to multiple workers")
			}
			seen[job.ID] = true
			first = job
		}
	}
	if len(seen) != jobs {
		t.Fatalf("claimed %d jobs, want %d", len(seen), jobs)
	}
	renewed, err := repo.Renew(f.ctx, first, time.Minute)
	requireOK(t, err)
	if renewed.Generation != first.Generation || renewed.LeaseUntil == nil {
		t.Fatal("invalid renewal")
	}
	_, err = f.pool.Exec(f.ctx, `UPDATE musicgetter.import_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, first.ID)
	requireOK(t, err)
	if err := repo.Complete(f.ctx, first); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("expired lease completed")
	}
	if _, err := repo.Renew(f.ctx, first, time.Minute); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("expired lease renewed")
	}
	n, err := repo.RequeueExpired(f.ctx, 2)
	requireOK(t, err)
	if n != 1 {
		t.Fatal("expired job not reclaimed")
	}
	reclaimed, err := repo.Claim(f.ctx, "new-worker", 1, time.Minute)
	requireOK(t, err)
	if len(reclaimed) != 1 || reclaimed[0].ID != first.ID || reclaimed[0].Generation <= first.Generation {
		t.Fatal("generation not fenced")
	}
	if err := repo.Complete(f.ctx, first); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("stale worker completed new lease")
	}
	requireOK(t, repo.Complete(f.ctx, reclaimed[0]))
	current, err := repo.Get(f.ctx, f.user.ID, first.ID)
	requireOK(t, err)
	if current.State != domain.JobCompleted || current.WorkerID != nil || current.LeaseUntil != nil {
		t.Fatal("completion left a lease")
	}
}

func TestJobReplayRetryBudgetAndParentConstraint(t *testing.T) {
	f := newFixture(t)
	repo := postgres.NewJobRepository(f.pool)
	record := f.importRecord(t, "jobs")
	item := f.item(t, record, "Retry")
	job := f.enqueue(t, item, 2)
	again := f.enqueue(t, item, 2)
	if job.ID != again.ID {
		t.Fatal("enqueue replay duplicated")
	}
	claimed, err := repo.Claim(f.ctx, "worker", 1, time.Minute)
	requireOK(t, err)
	if len(claimed) != 1 {
		t.Fatal("missing job")
	}
	requireOK(t, repo.Retry(f.ctx, claimed[0], time.Now().Add(time.Hour), "rate_limited"))
	ready, err := repo.Claim(f.ctx, "worker", 1, time.Minute)
	requireOK(t, err)
	if len(ready) != 0 {
		t.Fatal("retry scheduled before available_at")
	}
	_, err = f.pool.Exec(f.ctx, `UPDATE musicgetter.import_jobs SET available_at=clock_timestamp() WHERE id=$1`, job.ID)
	requireOK(t, err)
	claimed, err = repo.Claim(f.ctx, "worker", 1, time.Minute)
	requireOK(t, err)
	requireOK(t, repo.Retry(f.ctx, claimed[0], time.Now(), "unavailable"))
	current, err := repo.Get(f.ctx, f.user.ID, job.ID)
	requireOK(t, err)
	if current.State != domain.JobFailed || current.Attempts != 2 {
		t.Fatal("retry budget ignored")
	}
	replay := f.enqueue(t, item, 2)
	if replay.State != domain.JobFailed || replay.Attempts != 2 {
		t.Fatal("enqueue reset failed work")
	}
	other := f.importRecord(t, "other")
	otherItem := f.item(t, other, "Other")
	_, err = repo.Enqueue(f.ctx, domain.ImportJob{OwnerID: f.user.ID, ImportID: record.ID, ItemID: otherItem.ID, Kind: domain.JobMatch, LogicalKey: "wrong-parent"})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("job attached to another import's item")
	}
}

func TestCancelledImportAllowsOnlyReconciliation(t *testing.T) {
	f := newFixture(t)
	repo := postgres.NewJobRepository(f.pool)
	record := f.importRecord(t, "cancelled")
	item := f.item(t, record, "Song")
	f.enqueue(t, item, 2)
	_, err := repo.Enqueue(f.ctx, domain.ImportJob{OwnerID: f.user.ID, ImportID: record.ID, ItemID: item.ID, Kind: domain.JobReconcile, LogicalKey: "reconcile"})
	requireOK(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE musicgetter.imports SET state='cancelled' WHERE id=$1`, record.ID)
	requireOK(t, err)
	claimed, err := repo.Claim(f.ctx, "worker", 10, time.Minute)
	requireOK(t, err)
	if len(claimed) != 1 || claimed[0].Kind != domain.JobReconcile {
		t.Fatal("new work started for cancelled import")
	}
	requireOK(t, repo.Fail(f.ctx, claimed[0], "unsupported"))
}
