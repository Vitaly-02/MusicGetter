package api

import (
	"sync"
	"time"
)

type rateEntry struct {
	start time.Time
	count int
}

// Fixed one-minute windows, bounded memory, per process. A full map fails closed
// for new keys until old entries expire. No goroutine/timer per remote address.
type RateLimiter struct {
	mu        sync.Mutex
	entries   map[string]rateEntry
	maxKeys   int
	now       func() time.Time
	nextSweep time.Time
}

func NewRateLimiter(maxKeys int) *RateLimiter {
	if maxKeys < 1 {
		maxKeys = 10000
	}
	return &RateLimiter{entries: make(map[string]rateEntry), maxKeys: maxKeys, now: time.Now}
}
func (l *RateLimiter) Allow(key string, limit int) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if !now.Before(l.nextSweep) {
		for k, v := range l.entries {
			if now.Sub(v.start) >= time.Minute {
				delete(l.entries, k)
			}
		}
		l.nextSweep = now.Add(time.Minute)
	}
	entry, exists := l.entries[key]
	if !exists && len(l.entries) >= l.maxKeys {
		return false, 60
	}
	if !exists || now.Sub(entry.start) >= time.Minute {
		entry = rateEntry{start: now}
	}
	if entry.count >= limit {
		return false, max(1, int(entry.start.Add(time.Minute).Sub(now).Seconds())+1)
	}
	entry.count++
	l.entries[key] = entry
	return true, 0
}
