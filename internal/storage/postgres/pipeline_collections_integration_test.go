//go:build integration

package postgres_test

import (
	"fmt"
	"musicgetter/internal/domain"
	"musicgetter/internal/storage/postgres"
	"sync"
	"testing"
	"time"
)

func TestPipelineRepeatedCollectionKindsAndMappings(t *testing.T) {
	f := newFixture(t)
	d := newAtomic()
	p := pipeline(f, d)
	dr := postgres.NewDestinationCollectionRepository(f.pool)
	for _, kind := range []domain.SourceCollectionKind{domain.SourceFavorites, domain.SourcePlaylist, domain.SourceAlbum, domain.SourceSelection} {
		targetKind := domain.CollectionPlaylist
		if kind == domain.SourceFavorites {
			targetKind = domain.CollectionFavorites
		}
		if kind == domain.SourceAlbum {
			targetKind = domain.CollectionAlbum
		}
		target := f.destination
		if kind != domain.SourceFavorites {
			var err error
			target, err = dr.Ensure(f.ctx, domain.DestinationCollection{OwnerID: f.user.ID, ConnectionID: f.connection.ID, ExternalKey: string(kind), Kind: targetKind, Title: "Target"})
			requireOK(t, err)
		}
		src, err := postgres.NewSourceRepository(f.pool).EnsureCollection(f.ctx, domain.SourceCollection{OwnerID: f.user.ID, ProfileID: f.profile.ID, Source: f.profile.Source, CollectionKey: string(kind), Kind: kind, Title: "Collection"})
		requireOK(t, err)
		for repeat := range 2 {
			// One stable source ID is cached across all four collections/imports.
			key := "source-key"
			track := f.track(t, "Song", &key)
			i, err := postgres.NewImportRepository(f.pool).Create(f.ctx, f.user.ID, src.ID, target.ID, fmt.Sprintf("%s-%d", kind, repeat))
			requireOK(t, err)
			item, err := postgres.NewImportRepository(f.pool).AddItem(f.ctx, f.user.ID, i.ID, track.ID, 0)
			requireOK(t, err)
			replay, err := postgres.NewImportRepository(f.pool).AddItem(f.ctx, f.user.ID, i.ID, track.ID, 9)
			requireOK(t, err)
			if replay.ID != item.ID {
				t.Fatal("input duplicated")
			}
			f.enqueue(t, item, 3)
			requireOK(t, p.Process(f.ctx, claimOne(t, f)))
			assertState(t, f, i.ID, domain.ImportCompleted)
			items, err := postgres.NewImportRepository(f.pool).ListItems(f.ctx, f.user.ID, i.ID, nil, 10)
			requireOK(t, err)
			want := domain.ItemAdded
			if repeat == 1 {
				want = domain.ItemAlreadyPresent
			}
			if items[0].State != want {
				t.Fatalf("%s repeat %d: %s", kind, repeat, items[0].State)
			}
		}
	}
	if d.adds != 4 || d.calls != 4 || d.searches != 1 {
		t.Fatalf("adds=%d calls=%d searches=%d", d.adds, d.calls, d.searches)
	}
	var memberships int
	requireOK(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM musicgetter.destination_memberships`).Scan(&memberships))
	if memberships != 4 {
		t.Fatal("ledger duplicated")
	}
}

func TestPipelineConcurrentAliasesAndPreexistingMembership(t *testing.T) {
	f := newFixture(t)
	d := newAtomic()
	p := pipeline(f, d)
	for n := range 8 {
		i := f.importRecord(t, fmt.Sprint(n))
		key := fmt.Sprint(n)
		track := f.track(t, "Song", &key)
		item, err := postgres.NewImportRepository(f.pool).AddItem(f.ctx, f.user.ID, i.ID, track.ID, 0)
		requireOK(t, err)
		f.enqueue(t, item, 3)
	}
	jobs, err := postgres.NewJobRepository(f.pool).Claim(f.ctx, "concurrent", 8, time.Minute)
	requireOK(t, err)
	var g sync.WaitGroup
	errs := make(chan error, 8)
	for _, j := range jobs {
		g.Go(func() { errs <- p.Process(f.ctx, j) })
	}
	g.Wait()
	close(errs)
	for err := range errs {
		requireOK(t, err)
	}
	if d.adds != 1 {
		t.Fatalf("concurrent duplicates: %d", d.adds)
	}
	d.members["favorites/remote:existing"] = true
	i := f.importRecord(t, "preexisting")
	f.enqueue(t, f.item(t, i, "Existing"), 3)
	requireOK(t, p.Process(f.ctx, claimOne(t, f)))
	if d.adds != 1 {
		t.Fatal("preexisting membership added")
	}
}
