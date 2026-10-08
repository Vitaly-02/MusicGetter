package config

import (
	"fmt"
	"musicgetter/internal/matcher"
	"os"
	"strconv"
	"time"
)

func LoadMatcher() (matcher.Options, error) { return loadMatcher(os.LookupEnv) }
func loadMatcher(lookup func(string) (string, bool)) (matcher.Options, error) {
	o := matcher.DefaultOptions()
	for _, f := range []struct {
		k string
		p *int
	}{{"MATCHER_SEARCH_CONCURRENCY", &o.RemoteConcurrency}, {"MATCHER_CACHE_ENTRIES", &o.CacheEntries}, {"MATCHER_CACHE_BYTES", &o.CacheBytes}} {
		if v, ok := lookup(f.k); ok {
			n, err := strconv.Atoi(v)
			if err != nil {
				return o, fmt.Errorf("%s must be an integer", f.k)
			}
			*f.p = n
		}
	}
	for _, f := range []struct {
		k string
		p *time.Duration
	}{{"MATCHER_SEARCH_TIMEOUT", &o.RemoteTimeout}, {"MATCHER_CACHE_TTL", &o.CacheTTL}, {"MATCHER_NEGATIVE_TTL", &o.NegativeTTL}} {
		if v, ok := lookup(f.k); ok {
			d, err := time.ParseDuration(v)
			if err != nil {
				return o, fmt.Errorf("%s must be a duration", f.k)
			}
			*f.p = d
		}
	}
	if err := o.Validate(); err != nil {
		return o, fmt.Errorf("invalid matcher limits")
	}
	return o, nil
}
