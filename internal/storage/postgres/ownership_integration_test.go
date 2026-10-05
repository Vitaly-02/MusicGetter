//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	"musicgetter/internal/domain"
	"musicgetter/internal/storage/postgres"
)

func TestOwnershipAndConnectionConstraints(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	user2, err := postgres.NewUserRepository(f.pool).EnsureTelegram(ctx, 43)
	requireOK(t, err)
	canonical := f.track(t, "Song", nil)
	track := f.remote(t, "remote")
	_, err = postgres.NewCanonicalTrackRepository(f.pool).Get(ctx, user2.ID, canonical.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-user read")
	}
	_, err = postgres.NewCanonicalTrackRepository(f.pool).Ensure(ctx, domain.CanonicalTrackInput{OwnerID: user2.ID, ProfileID: f.profile.ID, Source: f.profile.Source, Metadata: domain.TrackMetadata{Title: "Other", Artists: []string{"Artist"}}})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("cross-owner profile accepted: %v", err)
	}
	_, err = f.pool.Exec(ctx, `INSERT INTO musicgetter.destination_memberships(owner_id,connection_id,collection_id,destination_track_id) VALUES ($1,$2,$3,$4)`, user2.ID, f.connection.ID, f.destination.ID, track.ID)
	requireSQLState(t, err, "23503")
	dr := postgres.NewDestinationCollectionRepository(f.pool)
	second, err := dr.EnsureConnection(ctx, domain.DestinationConnection{OwnerID: f.user.ID, Adapter: "fake", AccountKey: "second"})
	requireOK(t, err)
	secondTrack, err := postgres.NewDestinationTrackRepository(f.pool).Ensure(ctx, domain.DestinationTrack{OwnerID: f.user.ID, ConnectionID: second.ID, ExternalKey: track.ExternalKey, Metadata: track.Metadata})
	requireOK(t, err)
	_, err = postgres.NewMembershipRepository(f.pool).Reserve(ctx, f.user.ID, f.connection.ID, f.destination.ID, secondTrack.ID)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("cross-connection membership accepted")
	}
	_, err = dr.Ensure(ctx, domain.DestinationCollection{OwnerID: f.user.ID, ConnectionID: f.connection.ID, ExternalKey: "another-favorites", Kind: domain.CollectionFavorites, Title: "Favorites"})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatal("second favorites collection accepted")
	}
	_, err = dr.Ensure(ctx, domain.DestinationCollection{OwnerID: f.user.ID, ConnectionID: f.connection.ID, ExternalKey: "selection", Kind: domain.DestinationCollectionKind("selection"), Title: "Selection"})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("destination selection accepted")
	}
	sr := postgres.NewSourceRepository(f.pool)
	selection, err := sr.EnsureCollection(ctx, domain.SourceCollection{OwnerID: f.user.ID, ProfileID: f.profile.ID, Source: f.profile.Source, CollectionKey: "selection-id", Provisional: true, Kind: domain.SourceSelection, Title: "Selection"})
	requireOK(t, err)
	if selection.Kind != domain.SourceSelection {
		t.Fatal("selection not persisted")
	}
	_, err = sr.GetCollection(ctx, user2.ID, selection.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-user source read")
	}
	profile2, err := sr.EnsureProfile(ctx, domain.SourceProfile{OwnerID: f.user.ID, Source: domain.SourceVK, ProfileKey: "vk", Label: "VK"})
	requireOK(t, err)
	vkTrack, err := postgres.NewCanonicalTrackRepository(f.pool).Ensure(ctx, domain.CanonicalTrackInput{OwnerID: f.user.ID, ProfileID: profile2.ID, Source: profile2.Source, Metadata: canonical.Metadata})
	requireOK(t, err)
	record := f.importRecord(t, "tenant-check")
	_, err = postgres.NewImportRepository(f.pool).AddItem(ctx, f.user.ID, record.ID, vkTrack.ID, 0)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("source/profile mismatch allowed in import")
	}
}

func TestMappingDecisionCannotBeSilentlyChanged(t *testing.T) {
	f := newFixture(t)
	canonical := f.track(t, "Song", nil)
	a := f.remote(t, "a")
	b := f.remote(t, "b")
	repo := postgres.NewMappingRepository(f.pool)
	input := domain.TrackMapping{OwnerID: f.user.ID, CanonicalTrackID: canonical.ID, ConnectionID: f.connection.ID, DestinationTrackID: a.ID, Origin: domain.MappingManual, PolicyVersion: "v1"}
	first, err := repo.Save(f.ctx, input)
	requireOK(t, err)
	again, err := repo.Save(f.ctx, input)
	requireOK(t, err)
	if first.ID != again.ID {
		t.Fatal("mapping replay duplicated")
	}
	input.DestinationTrackID = b.ID
	_, err = repo.Save(f.ctx, input)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatal("accepted match overwritten")
	}
	persisted, err := repo.Get(f.ctx, f.user.ID, canonical.ID, f.connection.ID)
	requireOK(t, err)
	if persisted.DestinationTrackID != a.ID {
		t.Fatal("mapping changed after conflict")
	}
}
