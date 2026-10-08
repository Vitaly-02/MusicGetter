// Package destinationtest exposes behavioural tests reusable by future adapters.
// A fixture must use a fresh isolated account, favorites ID "favorites", and two
// searchable tracks "a"/"b" with artist "Artist". Never run against user libraries.
package destinationtest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"musicgetter/internal/destination"
	"musicgetter/internal/domain"
	"musicgetter/internal/matcher"
)

type Fixture struct {
	Adapter      destination.Destination
	ConnectionID domain.ID
}
type Factory func(*testing.T) Fixture

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func Run(t *testing.T, newFixture Factory) {
	t.Helper()
	t.Run("capabilities_and_search", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		c, err := f.Adapter.Capabilities(ctx)
		ok(t, err)
		ok(t, destination.CheckAdapter(f.Adapter, c))
		if !c.Search {
			t.Skip("search unsupported")
		}
		search := f.Adapter.(destination.TrackSearcher)
		page, err := search.SearchTracks(ctx, matcher.Query{Text: "artist", Limit: 1})
		ok(t, err)
		if len(page.Candidates) != 1 || page.NextCursor == "" {
			t.Fatal("search not paginated")
		}
		second, err := search.SearchTracks(ctx, matcher.Query{Text: "artist", Limit: 1, Cursor: page.NextCursor})
		ok(t, err)
		if len(second.Candidates) != 1 || second.Candidates[0].Track.ExternalKey == page.Candidates[0].Track.ExternalKey {
			t.Fatal("search cursor repeated item")
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		_, err = search.SearchTracks(cancelled, matcher.Query{Text: "artist", Limit: 1})
		if !errors.Is(err, context.Canceled) {
			t.Fatal("search ignored cancellation", err)
		}
	})
	t.Run("favorites_set_semantics", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		caps, err := f.Adapter.Capabilities(ctx)
		ok(t, err)
		if !caps.SupportsFavorites {
			t.Skip("favorites unsupported")
		}
		fav := f.Adapter.(destination.Favorites)
		_, err = fav.AddToFavorites(ctx, []destination.AddItem{{OperationKey: "existing", TrackID: "a"}})
		ok(t, err)
		request := destination.EnsureRequest{OperationKey: "repeat", TrackID: "a", Target: domain.Target{ConnectionID: f.ConnectionID, Kind: domain.CollectionFavorites, ExternalID: "favorites"}}
		effect, err := f.Adapter.EnsureTrack(ctx, request)
		ok(t, err)
		if effect.State != destination.EffectAlreadyPresent {
			t.Fatal("preexisting track added again")
		}
		_, err = f.Adapter.EnsureTrack(ctx, request)
		ok(t, err)
		request.TrackID = "b"
		_, err = f.Adapter.EnsureTrack(ctx, request)
		if !errors.Is(err, domain.ErrConflict) {
			t.Fatal("operation payload mutable", err)
		}
		page, err := fav.GetFavorites(ctx, destination.PageRequest{Limit: 1})
		ok(t, err)
		if len(page.Items) != 1 || page.Items[0].ExternalKey != "a" {
			t.Fatal("duplicate favorites")
		}
		page.Items[0].Metadata.Artists[0] = "mutated"
		page, err = fav.GetFavorites(ctx, destination.PageRequest{Limit: 1})
		ok(t, err)
		if page.Items[0].Metadata.Artists[0] != "Artist" {
			t.Fatal("read mutated remote state")
		}
		request.Target.ConnectionID = "another-account"
		_, err = f.Adapter.EnsureTrack(ctx, request)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("foreign connection accepted", err)
		}
	})
	t.Run("playlist_creation_and_name_ambiguity", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		caps, err := f.Adapter.Capabilities(ctx)
		ok(t, err)
		if !caps.SupportsPlaylists || !caps.IdempotentCreatePlaylist {
			t.Skip("idempotent playlist creation unsupported")
		}
		p := f.Adapter.(destination.Playlists)
		req := destination.CreateCollectionRequest{OperationKey: "create", Title: "Artist — Album"}
		var group sync.WaitGroup
		ids := make(chan string, 12)
		errs := make(chan error, 12)
		for range 12 {
			group.Go(func() { c, e := p.CreatePlaylist(ctx, req); ids <- c.Collection.ID; errs <- e })
		}
		group.Wait()
		close(ids)
		close(errs)
		for e := range errs {
			ok(t, e)
		}
		id := ""
		for v := range ids {
			if v == "" || id != "" && id != v {
				t.Fatal("concurrent create duplicated target")
			}
			id = v
		}
		req.Title = "Changed"
		_, err = p.CreatePlaylist(ctx, req)
		if !errors.Is(err, domain.ErrConflict) {
			t.Fatal("create key payload changed")
		}
		req = destination.CreateCollectionRequest{OperationKey: "other", Title: "Artist — Album"}
		other, err := p.CreatePlaylist(ctx, req)
		ok(t, err)
		if other.Collection.ID == id {
			t.Fatal("titles treated as identity")
		}
		found, err := p.FindPlaylist(ctx, destination.FindPlaylistRequest{Title: req.Title, Page: destination.PageRequest{Limit: 1}})
		ok(t, err)
		if len(found.Items) != 1 || found.NextCursor == "" {
			t.Fatal("ambiguous name silently collapsed")
		}
		next, err := p.FindPlaylist(ctx, destination.FindPlaylistRequest{Title: req.Title, Page: destination.PageRequest{Limit: 1, Cursor: found.NextCursor}})
		ok(t, err)
		if len(next.Items) != 1 || next.Items[0].ID == found.Items[0].ID {
			t.Fatal("find pagination")
		}
	})
	t.Run("concurrent_atomic_membership", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		caps, err := f.Adapter.Capabilities(ctx)
		ok(t, err)
		if !caps.SupportsFavorites || !caps.AtomicEnsureMembership {
			t.Skip("atomic favorites unsupported")
		}
		var wg sync.WaitGroup
		errs := make(chan error, 16)
		for n := range 16 {
			wg.Go(func() {
				_, e := f.Adapter.EnsureTrack(ctx, destination.EnsureRequest{OperationKey: fmt.Sprintf("concurrent-%d", n), TrackID: "a", Target: domain.Target{ConnectionID: f.ConnectionID, Kind: domain.CollectionFavorites, ExternalID: "favorites"}})
				errs <- e
			})
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			ok(t, e)
		}
		page, err := f.Adapter.(destination.Favorites).GetFavorites(ctx, destination.PageRequest{Limit: 200})
		ok(t, err)
		if len(page.Items) != 1 || page.NextCursor != "" {
			t.Fatal("concurrent atomic ensure duplicated membership")
		}
	})
	t.Run("bulk_results_and_membership", func(t *testing.T) { bulkContract(t, newFixture(t)) })
	t.Run("optional_album", func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		caps, err := f.Adapter.Capabilities(ctx)
		ok(t, err)
		albums, implemented := f.Adapter.(destination.Albums)
		if !caps.SupportsAlbumCollections {
			if implemented {
				_, err = albums.CreateAlbum(ctx, destination.CreateCollectionRequest{OperationKey: "album", Title: "Album"})
				if !errors.Is(err, destination.ErrUnsupported) {
					t.Fatal("disabled album accepted")
				}
			}
			return
		}
		if !implemented {
			t.Fatal("advertised album port missing")
		}
		c, err := albums.CreateAlbum(ctx, destination.CreateCollectionRequest{OperationKey: "album", Title: "Album"})
		ok(t, err)
		_, err = albums.AddTracksToAlbum(ctx, destination.AddRequest{CollectionID: c.Collection.ID, Items: []destination.AddItem{{OperationKey: "a", TrackID: "a"}}})
		ok(t, err)
		page, err := albums.GetAlbumTracks(ctx, c.Collection.ID, destination.PageRequest{Limit: 1})
		ok(t, err)
		if len(page.Items) != 1 {
			t.Fatal("album did not retain track")
		}
	})
}
func bulkContract(t *testing.T, f Fixture) {
	ctx := context.Background()
	caps, err := f.Adapter.Capabilities(ctx)
	ok(t, err)
	if !caps.SupportsPlaylists {
		t.Skip("playlists unsupported")
	}
	p := f.Adapter.(destination.Playlists)
	c, err := p.CreatePlaylist(ctx, destination.CreateCollectionRequest{OperationKey: "create", Title: "Playlist"})
	ok(t, err)
	items := []destination.AddItem{{OperationKey: "a", TrackID: "a"}, {OperationKey: "b", TrackID: "b"}}
	if caps.SupportsBulkAdd {
		out, err := p.AddTracksToPlaylist(ctx, destination.AddRequest{CollectionID: c.Collection.ID, Items: items})
		ok(t, err)
		if len(out.Items) != 2 || out.Items[0].OperationKey != "a" || out.Items[1].TrackID != "b" {
			t.Fatal("missing per-item receipts")
		}
	} else {
		for _, item := range items {
			_, err := p.AddTracksToPlaylist(ctx, destination.AddRequest{CollectionID: c.Collection.ID, Items: []destination.AddItem{item}})
			ok(t, err)
		}
	}
	for _, item := range items {
		_, err := p.AddTracksToPlaylist(ctx, destination.AddRequest{CollectionID: c.Collection.ID, Items: []destination.AddItem{item}})
		ok(t, err)
	}
	page, err := p.GetPlaylistTracks(ctx, c.Collection.ID, destination.PageRequest{Limit: 1})
	ok(t, err)
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatal("track pagination")
	}
	second, err := p.GetPlaylistTracks(ctx, c.Collection.ID, destination.PageRequest{Limit: 1, Cursor: page.NextCursor})
	ok(t, err)
	if len(second.Items) != 1 || page.Items[0].ExternalKey == second.Items[0].ExternalKey {
		t.Fatal("cursor duplicated track")
	}
	if caps.SupportsMembershipLookup {
		has, err := f.Adapter.(destination.MembershipReader).Contains(ctx, domain.Target{ConnectionID: f.ConnectionID, ExternalID: c.Collection.ID, Kind: domain.CollectionPlaylist}, "a")
		ok(t, err)
		if !has {
			t.Fatal("membership absent")
		}
	}
	if caps.ReconcileOperations {
		effect, err := f.Adapter.(destination.OperationReconciler).Reconcile(ctx, "a", "")
		ok(t, err)
		if effect.State != destination.EffectApplied {
			t.Fatal("reconciliation lost effect")
		}
	}
	_, err = p.GetPlaylistTracks(ctx, c.Collection.ID, destination.PageRequest{Limit: 201})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("unbounded page allowed")
	}
}
