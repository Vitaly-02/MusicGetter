package matcher

import (
	"context"
	"errors"
	"musicgetter/internal/domain"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheTTLNegativeIsolationAndCopies(t *testing.T) {
	o := DefaultOptions()
	o.CacheEntries = 2
	c := newSearchCache(o)
	now := time.Now()
	c.now = func() time.Time { return now }
	var calls int
	empty := false
	remote := searchFunc(func(context.Context, Query) (CandidatePage, error) {
		calls++
		if empty {
			return CandidatePage{}, nil
		}
		return CandidatePage{Candidates: []Candidate{candidate("a", song("Song", "Artist"))}}, nil
	})
	q := Strategies(song("Song", "Artist"))[0]
	get := func(owner, connection string) CandidatePage {
		t.Helper()
		p, err := c.get(context.Background(), id(owner), id(connection), remote, q)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := get("a", "x")
	p.Candidates[0].Track.Metadata.Artists[0] = "changed"
	*p.Candidates[0].Track.Metadata.DurationMS = 1
	p = get("a", "x")
	if calls != 1 || p.Candidates[0].Track.Metadata.Artists[0] != "Artist" || *p.Candidates[0].Track.Metadata.DurationMS != 180000 {
		t.Fatal("cache ownership")
	}
	now = now.Add(o.CacheTTL)
	get("a", "x")
	if calls != 2 {
		t.Fatal("TTL not expired")
	}
	get("b", "x")
	get("b", "y")
	if calls != 4 {
		t.Fatal("tenant/connection cache leak")
	}
	get("a", "x")
	if calls != 5 {
		t.Fatal("entry bound failed")
	}
	empty = true
	now = now.Add(o.CacheTTL)
	get("a", "x")
	get("a", "x")
	if calls != 6 {
		t.Fatal("negative cache missed")
	}
	now = now.Add(o.NegativeTTL)
	get("a", "x")
	if calls != 7 {
		t.Fatal("negative TTL not expired")
	}
}

func TestCacheCoalescesAndLimitsRemoteSearch(t *testing.T) {
	o := DefaultOptions()
	o.RemoteConcurrency = 2
	c := newSearchCache(o)
	var calls, active, maximum atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 20)
	remote := searchFunc(func(ctx context.Context, q Query) (CandidatePage, error) {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
			return CandidatePage{}, nil
		case <-ctx.Done():
			return CandidatePage{}, ctx.Err()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var g sync.WaitGroup
	errs := make(chan error, 20)
	q := Strategies(song("Song", "Artist"))[0]
	for range 20 {
		g.Go(func() { _, err := c.get(ctx, "owner", "connection", remote, q); errs <- err })
	}
	<-started
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatal("same request not coalesced", calls.Load())
	}
	close(release)
	g.Wait()
	for range 20 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	// Distinct keys compete for the same process-wide slots.
	release = make(chan struct{})
	for i := range 10 {
		g.Go(func() {
			query := q
			query.Text = string(rune('a' + i))
			_, err := c.get(ctx, "owner", "connection", remote, query)
			errs <- err
		})
	}
	<-started
	<-started
	time.Sleep(20 * time.Millisecond)
	if active.Load() != 2 {
		t.Fatal("concurrency not bounded", active.Load())
	}
	close(release)
	g.Wait()
	for range 10 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if maximum.Load() != 2 {
		t.Fatal("remote concurrency", maximum.Load())
	}
}

func TestCacheCancellationErrorAndPanicRelease(t *testing.T) {
	o := DefaultOptions()
	o.RemoteConcurrency = 1
	c := newSearchCache(o)
	q := Strategies(song("Song", "Artist"))[0]
	entered := make(chan struct{})
	var calls atomic.Int32
	remote := searchFunc(func(ctx context.Context, q Query) (CandidatePage, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			return CandidatePage{}, ctx.Err()
		}
		return CandidatePage{}, nil
	})
	leader, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.get(leader, "a", "b", remote, q); done <- err }()
	<-entered
	follower, stopFollower := context.WithTimeout(context.Background(), time.Second)
	defer stopFollower()
	joined := make(chan error, 1)
	go func() { _, err := c.get(follower, "a", "b", remote, q); joined <- err }()
	stop()
	if !errors.Is(<-done, context.Canceled) {
		t.Fatal("owner not cancelled")
	}
	if err := <-joined; err != nil {
		t.Fatal("leader cancellation poisoned follower", err)
	}
	for _, mode := range []string{"error", "panic"} {
		q.Text = mode
		failing := searchFunc(func(context.Context, Query) (CandidatePage, error) {
			if mode == "panic" {
				panic("SECRET")
			}
			return CandidatePage{}, errors.New("SECRET")
		})
		for range 2 {
			_, err := c.get(follower, "a", "b", failing, q)
			if !errors.Is(err, ErrSearchUnavailable) {
				t.Fatal(err)
			}
		}
	}
	q.Text = "after"
	if _, err := c.get(follower, "a", "b", remote, q); err != nil {
		t.Fatal("slot leaked", err)
	}
}

func id(s string) domain.ID { return domain.ID(s) }

func TestCacheByteBudgetAndLRU(t *testing.T) {
	o := DefaultOptions()
	o.CacheBytes = 1 << 20
	o.CacheEntries = 100
	c := newSearchCache(o)
	q := Strategies(song("Song", "Artist"))[0]
	calls := 0
	remote := searchFunc(func(context.Context, Query) (CandidatePage, error) {
		calls++
		p := CandidatePage{}
		for i := range 50 {
			m := song(strings.Repeat("x", 1024), "Artist")
			p.Candidates = append(p.Candidates, candidate(string(rune('a'+i)), m))
		}
		return p, nil
	})
	ctx := context.Background()
	for i := range 12 {
		q.Text = string(rune('a' + i))
		if _, err := c.get(ctx, "a", "b", remote, q); err != nil {
			t.Fatal(err)
		}
		if c.bytes > o.CacheBytes || c.lru.Len() > o.CacheEntries {
			t.Fatal("cache grew beyond budget")
		}
	}
	last := q
	if _, err := c.get(ctx, "a", "b", remote, last); err != nil {
		t.Fatal(err)
	}
	if calls != 12 {
		t.Fatal("recent cache miss")
	}
	q.Text = "a"
	if _, err := c.get(ctx, "a", "b", remote, q); err != nil {
		t.Fatal(err)
	}
	if calls != 13 {
		t.Fatal("old entry not evicted")
	}
}

func TestCacheWaitingCallerCancellationAndTimeout(t *testing.T) {
	o := DefaultOptions()
	o.RemoteConcurrency = 1
	o.RemoteTimeout = 100 * time.Millisecond
	c := newSearchCache(o)
	q := Strategies(song("Song", "Artist"))[0]
	started := make(chan struct{})
	leaderDone := make(chan error, 1)
	remote := searchFunc(func(ctx context.Context, q Query) (CandidatePage, error) {
		close(started)
		<-ctx.Done()
		return CandidatePage{}, ctx.Err()
	})
	go func() { _, err := c.get(context.Background(), "a", "b", remote, q); leaderDone <- err }()
	<-started
	// Joiner cancellation never cancels the original remote request.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.get(ctx, "a", "b", remote, q); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	other := q
	other.Text = "other"
	short, stop := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer stop()
	if _, err := c.get(short, "a", "b", remote, other); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("slot wait ignored context", err)
	}
	if err := <-leaderDone; !errors.Is(err, ErrSearchUnavailable) {
		t.Fatal("remote timeout not bounded", err)
	}
	if len(c.flights) != 0 || len(c.slots) != 0 || c.lru.Len() != 0 {
		t.Fatal("timeout cached or leaked slot")
	}
}
