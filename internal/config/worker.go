package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	importer "musicgetter/internal/import"
	"os"
	"strconv"
	"time"
)

func LoadWorker() (importer.WorkerOptions, error) { return loadWorker(os.LookupEnv) }
func loadWorker(lookup func(string) (string, bool)) (importer.WorkerOptions, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return importer.WorkerOptions{}, fmt.Errorf("worker identity generation failed")
	}
	o := importer.WorkerOptions{ID: hex.EncodeToString(nonce[:]), Concurrency: 4, Lease: 30 * time.Second, PollInterval: time.Second, JobTimeout: 2 * time.Minute, RetryBase: time.Second, RetryMax: 5 * time.Minute}
	if v, ok := lookup("WORKER_CONCURRENCY"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return o, fmt.Errorf("WORKER_CONCURRENCY must be an integer")
		}
		o.Concurrency = n
	}
	for _, f := range []struct {
		k string
		p *time.Duration
	}{{"WORKER_LEASE", &o.Lease}, {"WORKER_POLL_INTERVAL", &o.PollInterval}, {"WORKER_JOB_TIMEOUT", &o.JobTimeout}, {"WORKER_RETRY_BASE", &o.RetryBase}, {"WORKER_RETRY_MAX", &o.RetryMax}} {
		if v, ok := lookup(f.k); ok {
			d, err := time.ParseDuration(v)
			if err != nil {
				return o, fmt.Errorf("%s must be a duration", f.k)
			}
			*f.p = d
		}
	}
	if err := o.Validate(); err != nil {
		return o, fmt.Errorf("invalid worker limits")
	}
	return o, nil
}
