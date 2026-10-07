//go:build integration

package postgres_test

import (
	"errors"
	"musicgetter/internal/domain"
	"musicgetter/internal/storage/postgres"
	"testing"
)

func TestPipelineCrashAfterEffectAndLeaseFencing(t *testing.T) {
	f := newFixture(t)
	d := newAtomic()
	d.loseACK = true
	p := pipeline(f, d)
	i := f.importRecord(t, "crash")
	f.enqueue(t, f.item(t, i, "Song"), 3)
	first := claimOne(t, f)
	if err := p.Process(f.ctx, first); err == nil {
		t.Fatal("expected lost ACK")
	}
	var state string
	requireOK(t, f.pool.QueryRow(f.ctx, `SELECT state FROM musicgetter.destination_memberships`).Scan(&state))
	if state != "unknown" {
		t.Fatal("lost intent")
	}
	_, err := f.pool.Exec(f.ctx, `UPDATE musicgetter.import_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, first.ID)
	requireOK(t, err)
	_, err = postgres.NewJobRepository(f.pool).RequeueExpired(f.ctx, 10)
	requireOK(t, err)
	second := claimOne(t, f)
	if err = p.Store.Finish(f.ctx, first, domain.ItemAdded, ""); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("stale worker committed")
	}
	// A fresh pipeline instance resumes persisted matching and intent.
	requireOK(t, pipeline(f, d).Process(f.ctx, second))
	assertState(t, f, i.ID, domain.ImportCompleted)
	if d.adds != 1 || d.calls != 1 || d.searches != 1 {
		t.Fatalf("repeat effect after restart: %+v", d)
	}
}

func TestPipelineExhaustionAndUnsafeDestination(t *testing.T) {
	f := newFixture(t)
	d := newAtomic()
	d.unsafe = true
	p := pipeline(f, d)
	i := f.importRecord(t, "unsafe")
	f.enqueue(t, f.item(t, i, "Unsafe"), 1)
	requireOK(t, p.Process(f.ctx, claimOne(t, f)))
	assertState(t, f, i.ID, domain.ImportCompletedWithErrors)
	if d.calls != 0 {
		t.Fatal("unsafe destination called")
	}
	d.unsafe = false
	i = f.importRecord(t, "expired-last-attempt")
	f.enqueue(t, f.item(t, i, "Expire"), 1)
	j := claimOne(t, f)
	_, err := f.pool.Exec(f.ctx, `UPDATE musicgetter.import_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, j.ID)
	requireOK(t, err)
	_, err = postgres.NewJobRepository(f.pool).RequeueExpired(f.ctx, 10)
	requireOK(t, err)
	requireOK(t, postgres.NewPipelineRepository(f.pool).Repair(f.ctx, 10))
	assertState(t, f, i.ID, domain.ImportCompletedWithErrors)
}

func TestCancelledCrashedWorkerDoesNotLeaveUnclaimableReadyJob(t *testing.T) {
	f := newFixture(t)
	u := postgres.NewUploadRepository(f.pool)
	i, err := u.CreateUpload(f.ctx, f.user.ID, uploadRequest(f, "cancel-crash"))
	requireOK(t, err)
	_, err = u.AppendChunk(f.ctx, f.user.ID, i.ID, uploadChunk(0, "chunk", "Song"))
	requireOK(t, err)
	requireOK(t, u.CompleteUpload(f.ctx, f.user.ID, i.ID, uploadComplete(0, 1)))
	j := claimOne(t, f)
	requireOK(t, u.CancelUpload(f.ctx, f.user.ID, i.ID))
	_, err = f.pool.Exec(f.ctx, `UPDATE musicgetter.import_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, j.ID)
	requireOK(t, err)
	queue := postgres.NewJobRepository(f.pool)
	_, err = queue.RequeueExpired(f.ctx, 10)
	requireOK(t, err)
	after, err := queue.Get(f.ctx, f.user.ID, j.ID)
	requireOK(t, err)
	if after.State != domain.JobFailed || after.LastErrorCode != "import_cancelled" {
		t.Fatal("cancelled job resurrected", after.State)
	}
}
