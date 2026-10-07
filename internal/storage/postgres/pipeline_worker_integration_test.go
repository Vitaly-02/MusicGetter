//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
	"musicgetter/internal/storage/postgres"
	"testing"
	"time"
)

func TestWorkerConcurrencyHeartbeatShutdownAndRetries(t *testing.T) {
	f := newFixture(t)
	d := newAtomic()
	block := make(chan struct{})
	d.block = block
	p := pipeline(f, d)
	i := f.importRecord(t, "worker")
	for n := range 5 {
		f.enqueue(t, f.item(t, i, fmt.Sprintf("Song %d", n)), 2)
	}
	opts := importer.WorkerOptions{ID: "worker", Concurrency: 2, Lease: 600 * time.Millisecond, PollInterval: 20 * time.Millisecond, JobTimeout: 5 * time.Second, RetryBase: time.Millisecond, RetryMax: 10 * time.Millisecond}
	worker := importer.Worker{Queue: postgres.NewJobRepository(f.pool), Repairer: postgres.NewPipelineRepository(f.pool), Pipeline: p, Options: opts, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		d.mu.Lock()
		active := d.active
		d.mu.Unlock()
		if active == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(1200 * time.Millisecond) // greater than lease; heartbeat must protect both
	var attempts int
	requireOK(t, f.pool.QueryRow(f.ctx, `SELECT max(attempts) FROM musicgetter.import_jobs`).Scan(&attempts))
	if attempts != 1 {
		t.Fatal("heartbeat lost lease")
	}
	close(block)
	for {
		record, err := postgres.NewImportRepository(f.pool).Get(f.ctx, f.user.ID, i.ID)
		requireOK(t, err)
		if record.State == domain.ImportCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	d.mu.Lock()
	if d.maxActive != 2 {
		t.Errorf("concurrency %d", d.maxActive)
	}
	d.mu.Unlock()
	// Retry exhaustion atomically closes item/import; no zombie processing state.
	d.block = nil
	d.searchError = true
	i = f.importRecord(t, "retry")
	f.enqueue(t, f.item(t, i, "Fail"), 2)
	ctx, cancel = context.WithCancel(f.ctx)
	defer cancel()
	go func() { done <- worker.Run(ctx) }()
	deadline = time.Now().Add(5 * time.Second)
	for {
		record, err := postgres.NewImportRepository(f.pool).Get(f.ctx, f.user.ID, i.ID)
		requireOK(t, err)
		if record.State == domain.ImportCompletedWithErrors {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retry exhaustion not finalized")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}
