package config

import "testing"

func TestWorkerConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   map[string]string
		valid bool
	}{
		{"defaults", nil, true}, {"configured", map[string]string{"WORKER_CONCURRENCY": "8", "WORKER_LEASE": "1s"}, true},
		{"zero", map[string]string{"WORKER_CONCURRENCY": "0"}, false}, {"too large", map[string]string{"WORKER_CONCURRENCY": "100"}, false},
		{"lease", map[string]string{"WORKER_LEASE": "2ms"}, false}, {"retry", map[string]string{"WORKER_RETRY_BASE": "10m", "WORKER_RETRY_MAX": "1s"}, false},
		{"duration", map[string]string{"WORKER_JOB_TIMEOUT": "invalid"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWorker(func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok })
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
}
