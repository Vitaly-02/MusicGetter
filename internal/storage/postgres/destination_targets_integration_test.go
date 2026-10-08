//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"musicgetter/internal/destination"
	"musicgetter/internal/destination/fake"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
	"musicgetter/internal/storage/postgres"
	"musicgetter/migrations"
)

func albumJob(t *testing.T, f fixture, key string) (domain.Import, domain.ImportJob) {
	t.Helper()
	target, err := postgres.NewDestinationCollectionRepository(f.pool).Ensure(f.ctx, domain.DestinationCollection{OwnerID: f.user.ID, ConnectionID: f.connection.ID, ExternalKey: "logical-album", Kind: domain.CollectionAlbum, Title: "Album"})
	requireOK(t, err)
	i, err := postgres.NewImportRepository(f.pool).Create(f.ctx, f.user.ID, f.source.ID, target.ID, key)
	requireOK(t, err)
	f.enqueue(t, f.item(t, i, "Song A"), 5)
	return i, claimOne(t, f)
}

type capabilitiesOverride struct {
	*fake.Destination
	albums, create bool
}

func (d capabilitiesOverride) Capabilities(ctx context.Context) (destination.Capabilities, error) {
	c, e := d.Destination.Capabilities(ctx)
	c.SupportsAlbumCollections = d.albums
	c.IdempotentCreatePlaylist = d.create
	return c, e
}

func TestAlbumFallbackConcurrentIntentRestartAndPinnedTarget(t *testing.T) {
	f := newFixture(t)
	remote := referenceDestination(t, f, false)
	i, a := albumJob(t, f, "first")
	_, b := albumJob(t, f, "second")
	store := postgres.NewPipelineRepository(f.pool)
	plans := make([]importer.AlbumFallbackPlan, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for n, j := range []domain.ImportJob{a, b} {
		wg.Add(1)
		go func() { defer wg.Done(); plans[n], errs[n] = store.PrepareAlbumFallback(f.ctx, j) }()
	}
	wg.Wait()
	for _, e := range errs {
		requireOK(t, e)
	}
	if plans[0].OperationKey != plans[1].OperationKey || plans[0].Title != "Artist — Album" {
		t.Fatal("unstable creation intent")
	}
	remote.FailNext(fake.CreatePlaylist, fake.Fault{AfterApply: true})
	if err := destinationPipeline(t, f, remote).Process(f.ctx, a); !errors.Is(err, destination.ErrUnavailable) {
		t.Fatal("lost ACK not surfaced", err)
	}
	requireOK(t, store.Reschedule(f.ctx, a, time.Now(), "lost_ack", false))
	// Reconstruct fake and pipeline, retaining only the external account and DB.
	restarted := fake.New(remote.Destination)
	// Even newly available native albums cannot override an unresolved creation intent.
	requireOK(t, destinationPipeline(t, f, capabilitiesOverride{restarted, true, true}).Process(f.ctx, claimOne(t, f)))
	requireOK(t, destinationPipeline(t, f, restarted).Process(f.ctx, b))
	assertState(t, f, i.ID, domain.ImportCompleted)
	third, j := albumJob(t, f, "third")
	// A completed binding also survives a capabilities change.
	requireOK(t, destinationPipeline(t, f, capabilitiesOverride{restarted, true, false}).Process(f.ctx, j))
	record, err := postgres.NewImportRepository(f.pool).Get(f.ctx, f.user.ID, third.ID)
	requireOK(t, err)
	if record.ResolvedDestinationCollectionID == nil {
		t.Fatal("lost pinned target")
	}
	page, err := restarted.FindPlaylist(f.ctx, destination.FindPlaylistRequest{Title: "Artist — Album", Page: destination.PageRequest{Limit: 10}})
	requireOK(t, err)
	if len(page.Items) != 1 {
		t.Fatal("duplicate playlist after restart")
	}
	tracks, err := restarted.GetPlaylistTracks(f.ctx, page.Items[0].ID, destination.PageRequest{Limit: 10})
	requireOK(t, err)
	if len(tracks.Items) != 1 {
		t.Fatal("duplicate membership")
	}
	if err = postgres.Migrate(f.ctx, f.cfg, migrations.FS, "down"); err == nil {
		t.Fatal("rollback erased live creation ledger")
	}
	requireOK(t, postgres.Migrate(f.ctx, f.cfg, migrations.FS, "up"))
}

func TestAlbumFallbackRequiresSafeCreationAndFencedOwnership(t *testing.T) {
	f := newFixture(t)
	remote := referenceDestination(t, f, false)
	i, j := albumJob(t, f, "unsafe-create")
	requireOK(t, destinationPipeline(t, f, capabilitiesOverride{remote, false, false}).Process(f.ctx, j))
	assertState(t, f, i.ID, domain.ImportCompletedWithErrors)
	if remote.Calls(fake.CreatePlaylist) != 0 || remote.Calls(fake.Ensure) != 0 {
		t.Fatal("unsafe destination received writes")
	}
	_, j = albumJob(t, f, "fenced-create")
	store := postgres.NewPipelineRepository(f.pool)
	stale := j
	stale.Generation++
	if _, err := store.PrepareAlbumFallback(f.ctx, stale); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("unfenced creation", err)
	}
	foreign := j
	foreign.OwnerID = "00000000-0000-0000-0000-000000000001"
	if _, err := store.PrepareAlbumFallback(f.ctx, foreign); err == nil {
		t.Fatal("foreign owner created binding")
	}
	plan, err := store.PrepareAlbumFallback(f.ctx, j)
	requireOK(t, err)
	result, err := remote.CreatePlaylist(f.ctx, destination.CreateCollectionRequest{OperationKey: plan.OperationKey, Title: plan.Title})
	requireOK(t, err)
	if err = store.BindAlbumFallback(f.ctx, stale, result.Collection); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("unfenced binding", err)
	}
	requireOK(t, store.BindAlbumFallback(f.ctx, j, result.Collection))
	conflicting := result.Collection
	conflicting.ID = "another-playlist"
	if err = store.BindAlbumFallback(f.ctx, j, conflicting); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("binding overwritten", err)
	}
}

func TestAlbumFallbackCancelRetainsCreationEvidence(t *testing.T) {
	f := newFixture(t)
	remote := referenceDestination(t, f, false)
	target, err := postgres.NewDestinationCollectionRepository(f.pool).Ensure(f.ctx, domain.DestinationCollection{OwnerID: f.user.ID, ConnectionID: f.connection.ID, ExternalKey: "logical-album", Kind: domain.CollectionAlbum, Title: "Album"})
	requireOK(t, err)
	uploads := postgres.NewUploadRepository(f.pool)
	request := uploadRequest(f, "cancel-create")
	request.DestinationCollectionID = target.ID
	created, err := uploads.CreateUpload(f.ctx, f.user.ID, request)
	requireOK(t, err)
	chunk := uploadChunk(0, "chunk", "Song A")
	chunk.Tracks[0].Album = "Album"
	_, err = uploads.AppendChunk(f.ctx, f.user.ID, created.ID, chunk)
	requireOK(t, err)
	requireOK(t, uploads.CompleteUpload(f.ctx, f.user.ID, created.ID, uploadComplete(0, 1)))
	i, err := postgres.NewImportRepository(f.pool).Get(f.ctx, f.user.ID, created.ID)
	requireOK(t, err)
	j := claimOne(t, f)
	store := postgres.NewPipelineRepository(f.pool)
	plan, err := store.PrepareAlbumFallback(f.ctx, j)
	requireOK(t, err)
	// Remote has applied creation while cancellation races with its ACK.
	result, err := remote.CreatePlaylist(f.ctx, destination.CreateCollectionRequest{OperationKey: plan.OperationKey, Title: plan.Title})
	requireOK(t, err)
	requireOK(t, postgres.NewUploadRepository(f.pool).CancelUpload(f.ctx, f.user.ID, i.ID))
	requireOK(t, store.BindAlbumFallback(f.ctx, j, result.Collection))
	if _, err = store.Load(f.ctx, j); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("cancelled import can deliver", err)
	}
	if remote.Calls(fake.Ensure) != 0 {
		t.Fatal("cancelled import added tracks")
	}
	var key string
	requireOK(t, f.pool.QueryRow(f.ctx, `SELECT operation_key FROM musicgetter.destination_target_bindings WHERE requested_collection_id=$1`, i.DestinationCollectionID).Scan(&key))
	if key != plan.OperationKey {
		t.Fatal("cancellation forgot creation intent")
	}
}
