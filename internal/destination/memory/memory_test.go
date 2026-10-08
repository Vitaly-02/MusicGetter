package memory_test

import (
	"context"
	"errors"
	"musicgetter/internal/destination"
	"musicgetter/internal/destination/destinationtest"
	"musicgetter/internal/destination/memory"
	"musicgetter/internal/domain"
	"testing"
)

func fixture(t *testing.T, albums bool) *memory.Destination {
	t.Helper()
	caps := memory.FullCapabilities()
	caps.SupportsAlbumCollections = albums
	d, err := memory.New(memory.Config{OwnerID: "owner", ConnectionID: "connection", Capabilities: caps, Tracks: []domain.DestinationTrack{{ExternalKey: "a", Metadata: domain.TrackMetadata{Title: "A", Artists: []string{"Artist"}}}, {ExternalKey: "b", Metadata: domain.TrackMetadata{Title: "B", Artists: []string{"Artist"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestContracts(t *testing.T) {
	for _, albums := range []bool{true, false} {
		name := "albums"
		if !albums {
			name = "playlist_only"
		}
		t.Run(name, func(t *testing.T) {
			destinationtest.Run(t, func(t *testing.T) destinationtest.Fixture {
				return destinationtest.Fixture{Adapter: fixture(t, albums), ConnectionID: "connection"}
			})
		})
	}
}
func TestPartialBulkReplayAndCursorScope(t *testing.T) {
	d := fixture(t, true)
	ctx := context.Background()
	p, err := d.CreatePlaylist(ctx, destination.CreateCollectionRequest{OperationKey: "create", Title: "P"})
	if err != nil {
		t.Fatal(err)
	}
	r := destination.AddRequest{CollectionID: p.Collection.ID, Items: []destination.AddItem{{OperationKey: "a", TrackID: "a"}, {OperationKey: "missing", TrackID: "missing"}}}
	out, err := d.AddTracksToPlaylist(ctx, r)
	if !errors.Is(err, domain.ErrNotFound) || len(out.Items) != 1 {
		t.Fatal("partial result discarded", out, err)
	}
	r.Items[1] = destination.AddItem{OperationKey: "b", TrackID: "b"}
	out, err = d.AddTracksToPlaylist(ctx, r)
	if err != nil || len(out.Items) != 2 {
		t.Fatal(out, err)
	}
	page, err := d.GetPlaylistTracks(ctx, p.Collection.ID, destination.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.GetFavorites(ctx, destination.PageRequest{Limit: 1, Cursor: page.NextCursor}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("cross-target cursor accepted")
	}
	if _, err = fixture(t, true).GetPlaylistTracks(ctx, p.Collection.ID, destination.PageRequest{Limit: 1}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("state shared across accounts")
	}
}
