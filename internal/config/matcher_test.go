package config

import "testing"

func TestMatcherConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   map[string]string
		valid bool
	}{
		{"defaults", nil, true}, {"configured", map[string]string{"MATCHER_SEARCH_CONCURRENCY": "2", "MATCHER_CACHE_TTL": "2m", "MATCHER_NEGATIVE_TTL": "10s"}, true},
		{"concurrency", map[string]string{"MATCHER_SEARCH_CONCURRENCY": "0"}, false},
		{"memory", map[string]string{"MATCHER_CACHE_BYTES": "9999999999"}, false},
		{"entries", map[string]string{"MATCHER_CACHE_ENTRIES": "0"}, false},
		{"ttl", map[string]string{"MATCHER_CACHE_TTL": "10s", "MATCHER_NEGATIVE_TTL": "20s"}, false},
		{"timeout", map[string]string{"MATCHER_SEARCH_TIMEOUT": "0s"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadMatcher(func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok })
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
		})
	}
}
