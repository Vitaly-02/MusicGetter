//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"musicgetter/internal/destination"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
	"musicgetter/internal/matcher"
	"musicgetter/internal/storage/postgres"
	"sync"
	"testing"
	"time"
)

// Contract fake: atomic set semantics including concurrent and preexisting writes.
// It exercises the pipeline with real PG; it is not a real destination adapter.
type atomicDestination struct {
	mu                    sync.Mutex
	members               map[string]bool
	calls, adds, searches int
	keys                  map[string]string
	loseACK               bool
	unsafe                bool
	searchError           bool
	block                 <-chan struct{}
	active, maxActive     int
}

func newAtomic() *atomicDestination {
	return &atomicDestination{members: map[string]bool{}, keys: map[string]string{}}
}
func (d *atomicDestination) Capabilities(context.Context) (destination.Capabilities, error) {
	return destination.Capabilities{AtomicEnsureMembership: !d.unsafe, ReadMembership: true, Search: true, TargetKinds: []domain.DestinationCollectionKind{domain.CollectionFavorites, domain.CollectionPlaylist, domain.CollectionAlbum}}, nil
}
func (d *atomicDestination) SearchTracks(ctx context.Context, q matcher.Query) (matcher.CandidatePage, error) {
	d.mu.Lock()
	d.searches++
	d.active++
	d.maxActive = max(d.maxActive, d.active)
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.active--; d.mu.Unlock() }()
	if d.block != nil {
		select {
		case <-d.block:
		case <-ctx.Done():
			return matcher.CandidatePage{}, ctx.Err()
		}
	}
	if d.searchError {
		return matcher.CandidatePage{}, errors.New("temporary")
	}
	return matcher.CandidatePage{Candidates: []matcher.Candidate{{Track: domain.DestinationTrack{ExternalKey: "remote:" + domain.NormalizeMetadataText(q.Track.Title), Metadata: q.Track}}}}, nil
}
func (d *atomicDestination) Contains(_ context.Context, t domain.Target, key string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.members[t.ExternalID+"/"+key], nil
}
func (d *atomicDestination) EnsureTrack(_ context.Context, r destination.EnsureRequest) (destination.Effect, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	key := r.Target.ExternalID + "/" + r.TrackID
	if old, ok := d.keys[key]; ok && old != r.OperationKey {
		return destination.Effect{}, errors.New("operation key changed")
	}
	d.keys[key] = r.OperationKey
	if d.members[key] {
		return destination.Effect{State: destination.EffectAlreadyPresent}, nil
	}
	d.members[key] = true
	d.adds++
	if d.loseACK {
		d.loseACK = false
		return destination.Effect{}, errors.New("lost ack")
	}
	return destination.Effect{State: destination.EffectApplied}, nil
}
func pipeline(f fixture, d *atomicDestination) *importer.Pipeline {
	engine, err := matcher.NewEngine(matcher.DefaultOptions())
	if err != nil {
		panic(err)
	}
	return &importer.Pipeline{Store: postgres.NewPipelineRepository(f.pool), Matcher: engine, Resolver: importer.Registry{"fake": func(context.Context, domain.DestinationConnection) (importer.Binding, error) {
		return importer.Binding{Destination: d, Catalog: d}, nil
	}}}
}
func claimOne(t *testing.T, f fixture) domain.ImportJob {
	t.Helper()
	jobs, err := postgres.NewJobRepository(f.pool).Claim(f.ctx, "test-worker", 1, time.Minute)
	requireOK(t, err)
	if len(jobs) != 1 {
		t.Fatalf("claimed %d jobs", len(jobs))
	}
	return jobs[0]
}
func assertState(t *testing.T, f fixture, id domain.ID, want domain.ImportState) {
	t.Helper()
	i, err := postgres.NewImportRepository(f.pool).Get(f.ctx, f.user.ID, id)
	requireOK(t, err)
	if i.State != want {
		t.Fatalf("state %s want %s", i.State, want)
	}
}
