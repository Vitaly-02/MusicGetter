package fake_test

import (
	"context"
	"errors"
	"musicgetter/internal/destination"
	"musicgetter/internal/destination/destinationtest"
	"musicgetter/internal/destination/fake"
	"musicgetter/internal/destination/memory"
	"musicgetter/internal/domain"
	"testing"
)

func fixture(t *testing.T) *fake.Destination {
	t.Helper()
	d, err := memory.New(memory.Config{OwnerID: "owner", ConnectionID: "connection", Capabilities: memory.FullCapabilities(), Tracks: []domain.DestinationTrack{{ExternalKey: "a", Metadata: domain.TrackMetadata{Title: "A", Artists: []string{"Artist"}}}, {ExternalKey: "b", Metadata: domain.TrackMetadata{Title: "B", Artists: []string{"Artist"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return fake.New(d)
}
func TestContracts(t *testing.T) {
	destinationtest.Run(t, func(t *testing.T) destinationtest.Fixture {
		return destinationtest.Fixture{Adapter: fixture(t), ConnectionID: "connection"}
	})
}
func TestLostCreateAndWriteResponses(t *testing.T) {
	d := fixture(t)
	ctx := context.Background()
	d.FailNext(fake.CreatePlaylist, fake.Fault{AfterApply: true})
	req := destination.CreateCollectionRequest{OperationKey: "create", Title: "Playlist"}
	if _, err := d.CreatePlaylist(ctx, req); !errors.Is(err, destination.ErrUnavailable) {
		t.Fatal(err)
	}
	p, err := d.CreatePlaylist(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := d.ListPlaylists(ctx, destination.PageRequest{Limit: 10})
	if err != nil || len(pages.Items) != 1 {
		t.Fatal("lost ACK duplicated playlist")
	}
	write := destination.EnsureRequest{OperationKey: "add", Target: domain.Target{ConnectionID: "connection", ExternalID: p.Collection.ID, Kind: domain.CollectionPlaylist}, TrackID: "a"}
	d.FailNext(fake.Ensure, fake.Fault{AfterApply: true})
	if _, err = d.EnsureTrack(ctx, write); !errors.Is(err, destination.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err = d.EnsureTrack(ctx, write); err != nil {
		t.Fatal(err)
	}
	page, err := d.GetPlaylistTracks(ctx, p.Collection.ID, destination.PageRequest{Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("lost ACK duplicated track")
	}
	d.FailNext(fake.Ensure, fake.Fault{})
	write.OperationKey = "other"
	write.TrackID = "b"
	_, _ = d.EnsureTrack(ctx, write)
	page, err = d.GetPlaylistTracks(ctx, p.Collection.ID, destination.PageRequest{Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("before-apply failure changed remote state")
	}
}
