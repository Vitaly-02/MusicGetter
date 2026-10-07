package importer

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"musicgetter/internal/domain"
	"sync"
	"time"
)

type WorkerQueue interface {
	JobQueue
	RequeueExpired(context.Context, int) (int64, error)
}
type Repairer interface {
	Repair(context.Context, int) error
}
type WorkerOptions struct {
	ID                                                   string
	Concurrency                                          int
	Lease, PollInterval, JobTimeout, RetryBase, RetryMax time.Duration
}

func (o WorkerOptions) Validate() error {
	if o.ID == "" || len(o.ID) > 100 || o.Concurrency < 1 || o.Concurrency > 64 || o.Lease < 300*time.Millisecond || o.Lease > time.Hour || o.PollInterval <= 0 || o.JobTimeout <= 0 || o.RetryBase <= 0 || o.RetryMax < o.RetryBase || o.RetryMax > 24*time.Hour {
		return domain.ErrInvalid
	}
	return nil
}

type Worker struct {
	Queue    WorkerQueue
	Repairer Repairer
	Pipeline *Pipeline
	Options  WorkerOptions
	Logger   *slog.Logger
}

func (w *Worker) Run(ctx context.Context) error {
	if err := w.Options.Validate(); err != nil {
		return err
	}
	if w.Queue == nil || w.Repairer == nil || w.Pipeline == nil || w.Logger == nil {
		return domain.ErrInvalid
	}
	var group sync.WaitGroup
	for range w.Options.Concurrency {
		group.Go(func() { w.loop(ctx) })
	}
	group.Go(func() {
		for ctx.Err() == nil {
			if _, err := w.Queue.RequeueExpired(ctx, 200); err != nil && ctx.Err() == nil {
				w.Logger.ErrorContext(ctx, "job_reaper_failed")
			}
			if err := w.Repairer.Repair(ctx, 200); err != nil && ctx.Err() == nil {
				w.Logger.ErrorContext(ctx, "job_repair_failed")
			}
			if !wait(ctx, w.Options.PollInterval) {
				return
			}
		}
	})
	group.Wait()
	return ctx.Err()
}
func (w *Worker) loop(ctx context.Context) {
	for ctx.Err() == nil {
		jobs, err := w.Queue.Claim(ctx, w.Options.ID, 1, w.Options.Lease)
		if err != nil {
			if ctx.Err() == nil {
				w.Logger.ErrorContext(ctx, "job_claim_failed")
			}
			if !wait(ctx, w.Options.PollInterval) {
				return
			}
			continue
		}
		if len(jobs) == 0 {
			if !wait(ctx, w.Options.PollInterval) {
				return
			}
			continue
		}
		w.runJob(ctx, jobs[0])
	}
}
func (w *Worker) runJob(parent context.Context, j domain.ImportJob) {
	ctx, cancel := context.WithTimeout(parent, w.Options.JobTimeout)
	done := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		tick := time.NewTicker(w.Options.Lease / 3)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				renewCtx, stop := context.WithTimeout(ctx, w.Options.Lease/3)
				_, err := w.Queue.Renew(renewCtx, j, w.Options.Lease)
				stop()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	err := w.process(ctx, j)
	close(done)
	<-heartbeatDone
	cancel()
	if err == nil {
		return
	}
	// A stopped process leaves its lease for recovery; never spend retries during shutdown.
	if parent.Err() != nil {
		return
	}
	if errors.Is(err, domain.ErrLeaseLost) {
		return
	}
	permanent := errors.Is(err, domain.ErrInvalid) || errors.Is(err, domain.ErrConflict) || errors.Is(err, ErrUnsupportedDestination)
	code := "temporary_failure"
	if permanent {
		code = "pipeline_rejected"
	}
	cleanup, stop := context.WithTimeout(parent, 5*time.Second)
	defer stop()
	err = w.Pipeline.Store.Reschedule(cleanup, j, time.Now().Add(Backoff(j.Attempts, w.Options.RetryBase, w.Options.RetryMax)), code, permanent)
	if err != nil && !errors.Is(err, domain.ErrLeaseLost) {
		w.Logger.ErrorContext(parent, "job_reschedule_failed", "job_id", j.ID)
	}
}
func (w *Worker) process(ctx context.Context, j domain.ImportJob) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("pipeline_panic")
		}
	}()
	return w.Pipeline.Process(ctx, j)
}

// Equal jitter: [half of capped exponential, capped exponential), no overflow.
func Backoff(attempt int, base, cap time.Duration) time.Duration {
	if base <= 0 || cap <= 0 {
		return 0
	}
	d := min(base, cap)
	for i := 1; i < attempt && d < cap; i++ {
		if d > cap/2 {
			d = cap
		} else {
			d *= 2
		}
	}
	half := d / 2
	if half == 0 {
		return d
	}
	return half + time.Duration(rand.Int64N(int64(d-half)))
}
func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
