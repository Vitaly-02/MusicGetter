package matcher

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"musicgetter/internal/domain"
)

type cacheEntry struct {
	key     [32]byte
	page    CandidatePage
	expires time.Time
	bytes   int
}
type searchFlight struct {
	done chan struct{}
	page CandidatePage
	err  error
}
type searchCache struct {
	mu      sync.Mutex
	entries map[[32]byte]*list.Element
	lru     *list.List
	bytes   int
	flights map[[32]byte]*searchFlight
	slots   chan struct{}
	options Options
	now     func() time.Time
}

func newSearchCache(o Options) *searchCache {
	return &searchCache{entries: map[[32]byte]*list.Element{}, lru: list.New(), flights: map[[32]byte]*searchFlight{}, slots: make(chan struct{}, o.RemoteConcurrency), options: o, now: time.Now}
}

func (c *searchCache) get(ctx context.Context, owner, connection domain.ID, remote Catalog, q Query) (CandidatePage, error) {
	encoded, _ := json.Marshal(struct {
		Owner, Connection domain.ID
		Policy            string
		Query             Query
	}{owner, connection, PolicyVersion, q})
	key := sha256.Sum256(encoded)
	for {
		if err := ctx.Err(); err != nil {
			return CandidatePage{}, err
		}
		c.mu.Lock()
		page, ok := c.lookup(key)
		flight := c.flights[key]
		c.mu.Unlock()
		if ok {
			return clonePage(page), nil
		}
		if flight != nil {
			page, err := awaitFlight(ctx, flight)
			if ctx.Err() == nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
				continue
			} // owner cancellation is not another caller's cancellation
			return page, err
		}
		select {
		case <-ctx.Done():
			return CandidatePage{}, ctx.Err()
		case c.slots <- struct{}{}:
		}
		c.mu.Lock()
		page, ok = c.lookup(key)
		flight = c.flights[key]
		if ok || flight != nil {
			c.mu.Unlock()
			<-c.slots
			continue
		}
		flight = &searchFlight{done: make(chan struct{})}
		c.flights[key] = flight
		c.mu.Unlock()
		page, size, err := c.fetch(ctx, remote, q)
		c.mu.Lock()
		if err == nil {
			c.insert(key, page, size)
		}
		flight.page, flight.err = page, err
		delete(c.flights, key)
		close(flight.done)
		c.mu.Unlock()
		<-c.slots
		return clonePage(page), err
	}
}
func awaitFlight(ctx context.Context, f *searchFlight) (CandidatePage, error) {
	select {
	case <-ctx.Done():
		return CandidatePage{}, ctx.Err()
	case <-f.done:
		return clonePage(f.page), f.err
	}
}
func (c *searchCache) lookup(key [32]byte) (CandidatePage, bool) {
	if node, ok := c.entries[key]; ok {
		e := node.Value.(cacheEntry)
		if c.now().Before(e.expires) {
			c.lru.MoveToFront(node)
			return e.page, true
		}
		c.remove(node)
	}
	return CandidatePage{}, false
}
func (c *searchCache) insert(key [32]byte, page CandidatePage, size int) {
	ttl := c.options.CacheTTL
	if len(page.Candidates) == 0 {
		ttl = c.options.NegativeTTL
	}
	size += 256
	if size > c.options.CacheBytes {
		return
	}
	for c.lru.Len() >= c.options.CacheEntries || c.bytes+size > c.options.CacheBytes {
		c.remove(c.lru.Back())
	}
	e := cacheEntry{key: key, page: clonePage(page), expires: c.now().Add(ttl), bytes: size}
	c.entries[key] = c.lru.PushFront(e)
	c.bytes += size
}
func (c *searchCache) remove(node *list.Element) {
	e := node.Value.(cacheEntry)
	delete(c.entries, e.key)
	c.bytes -= e.bytes
	c.lru.Remove(node)
}

func (c *searchCache) fetch(ctx context.Context, remote Catalog, q Query) (page CandidatePage, size int, err error) {
	// A panicking adapter must not leak a semaphore slot or strand joined callers.
	defer func() {
		if recover() != nil {
			page = CandidatePage{}
			size = 0
			err = ErrSearchUnavailable
		}
	}()
	request, cancel := context.WithTimeout(ctx, c.options.RemoteTimeout)
	defer cancel()
	q.Track = cloneMetadata(q.Track)
	raw, err := remote.SearchTracks(request, q)
	if ctx.Err() != nil {
		return CandidatePage{}, 0, ctx.Err()
	}
	if err != nil || request.Err() != nil {
		return CandidatePage{}, 0, ErrSearchUnavailable
	}
	if len(raw.Candidates) > q.Limit || len(raw.NextCursor) > 1024 {
		return CandidatePage{}, 0, ErrInvalidSearchPage
	}
	page.NextCursor = raw.NextCursor
	for _, candidate := range raw.Candidates {
		t := candidate.Track
		if len(t.ExternalKey) == 0 || len(t.ExternalKey) > 512 || domain.ValidateMetadata(t.Metadata) != nil {
			return CandidatePage{}, 0, ErrInvalidSearchPage
		}
		// Strip remote scoring/reasons and account/DB identity. Retain only metadata.
		page.Candidates = append(page.Candidates, Candidate{Track: domain.DestinationTrack{ExternalKey: t.ExternalKey, Metadata: cloneMetadata(t.Metadata)}})
	}
	encoded, err := json.Marshal(page)
	if err != nil || len(encoded) > 1<<20 {
		return CandidatePage{}, 0, ErrInvalidSearchPage
	}
	// Conservative accounting includes Go object/slice overhead, not just JSON.
	return page, 3*len(encoded) + 1024*len(page.Candidates), nil
}
