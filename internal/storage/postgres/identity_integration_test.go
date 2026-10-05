//go:build integration

package postgres_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"musicgetter/internal/domain"
	"musicgetter/internal/storage/postgres"
)

func TestCanonicalIdentityAndPhysicalUniqueness(t *testing.T) {
	f := newFixture(t)
	repo := postgres.NewCanonicalTrackRepository(f.pool)
	input := domain.CanonicalTrackInput{OwnerID: f.user.ID, ProfileID: f.profile.ID, Source: f.profile.Source, Metadata: domain.TrackMetadata{Title: " Song ", Artists: []string{"B", "A"}, Album: "Album"}}
	const count = 12
	values := make([]domain.CanonicalTrack, count)
	errs := make([]error, count)
	var group sync.WaitGroup
	for i := range count {
		group.Go(func() { values[i], errs[i] = repo.Ensure(f.ctx, input) })
	}
	group.Wait()
	for i := range count {
		requireOK(t, errs[i])
		if values[i].ID != values[0].ID {
			t.Fatal("concurrent fallback duplicates")
		}
	}
	input.Metadata.Title = "song"
	input.Metadata.Artists = []string{"a", "b", "a"}
	repeated, err := repo.Ensure(f.ctx, input)
	requireOK(t, err)
	if repeated.ID != values[0].ID || repeated.Metadata.Title != " Song " {
		t.Fatal("normalization replay rewrote evidence")
	}
	keyA, keyB := "a", "b"
	input.SourceTrackKey = &keyA
	a, err := repo.Ensure(f.ctx, input)
	requireOK(t, err)
	input.SourceTrackKey = &keyB
	b, err := repo.Ensure(f.ctx, input)
	requireOK(t, err)
	if a.ID == b.ID || a.ID == repeated.ID {
		t.Fatal("distinct stable IDs merged by metadata")
	}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO musicgetter.canonical_tracks
 (owner_id,profile_id,source,title,artists,album,duration_ms,edition,normalized_title,normalized_artists,fingerprint)
 SELECT owner_id,profile_id,source,title,artists,album,duration_ms,edition,normalized_title,normalized_artists,fingerprint
 FROM musicgetter.canonical_tracks WHERE id=$1`, repeated.ID)
	requireSQLState(t, err, "23505")
	zero := int64(0)
	input.SourceTrackKey = nil
	input.Metadata.DurationMS = &zero
	different, err := repo.Ensure(f.ctx, input)
	requireOK(t, err)
	if different.ID == repeated.ID {
		t.Fatal("unknown duration merged with zero")
	}
}

func TestImportReplayAndUniqueItems(t *testing.T) {
	f := newFixture(t)
	repo := postgres.NewImportRepository(f.pool)
	record := f.importRecord(t, "request-1")
	again := f.importRecord(t, "request-1")
	if again.ID != record.ID {
		t.Fatal("import request replay duplicated")
	}
	track := f.track(t, "Song", nil)
	first, err := repo.AddItem(f.ctx, f.user.ID, record.ID, track.ID, 4)
	requireOK(t, err)
	const count = 16
	items := make([]domain.ImportItem, count)
	errs := make([]error, count)
	var group sync.WaitGroup
	for i := range count {
		group.Go(func() { items[i], errs[i] = repo.AddItem(f.ctx, f.user.ID, record.ID, track.ID, 99) })
	}
	group.Wait()
	for i := range count {
		requireOK(t, errs[i])
		if items[i].ID != first.ID || items[i].Position != 4 {
			t.Fatal("item replay duplicated or changed position")
		}
	}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO musicgetter.import_items(owner_id,import_id,canonical_track_id,profile_id,source,connection_id,position)
 SELECT owner_id,import_id,canonical_track_id,profile_id,source,connection_id,position FROM musicgetter.import_items WHERE id=$1`, first.ID)
	requireSQLState(t, err, "23505")
	otherImport := f.importRecord(t, "request-2")
	other, err := repo.AddItem(f.ctx, f.user.ID, otherImport.ID, track.ID, 0)
	requireOK(t, err)
	if other.ID == first.ID {
		t.Fatal("different imports must have separate items")
	}
	dr := postgres.NewDestinationCollectionRepository(f.pool)
	playlist, err := dr.Ensure(f.ctx, domain.DestinationCollection{OwnerID: f.user.ID, ConnectionID: f.connection.ID, ExternalKey: "playlist", Kind: domain.CollectionPlaylist, Title: "Playlist"})
	requireOK(t, err)
	_, err = repo.Create(f.ctx, f.user.ID, f.source.ID, playlist.ID, "request-1")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed idempotency payload: %v", err)
	}
}

func TestMembershipDeduplicationAcrossImportsAndKinds(t *testing.T) {
	f := newFixture(t)
	repo := postgres.NewMembershipRepository(f.pool)
	track := f.remote(t, "remote-1")
	record1 := f.importRecord(t, "one")
	record2 := f.importRecord(t, "two")
	f.item(t, record1, "Song")
	f.item(t, record2, "Song")
	for _, kind := range []domain.DestinationCollectionKind{domain.CollectionFavorites, domain.CollectionPlaylist, domain.CollectionAlbum} {
		t.Run(string(kind), func(t *testing.T) {
			collection := f.destination
			if kind != domain.CollectionFavorites {
				var err error
				collection, err = postgres.NewDestinationCollectionRepository(f.pool).Ensure(f.ctx, domain.DestinationCollection{OwnerID: f.user.ID, ConnectionID: f.connection.ID, ExternalKey: string(kind), Kind: kind, Title: string(kind)})
				requireOK(t, err)
			}
			const count = 12
			members := make([]domain.DestinationMembership, count)
			errs := make([]error, count)
			var group sync.WaitGroup
			for i := range count {
				group.Go(func() { members[i], errs[i] = repo.Reserve(f.ctx, f.user.ID, f.connection.ID, collection.ID, track.ID) })
			}
			group.Wait()
			for i := range count {
				requireOK(t, errs[i])
				if members[i].ID != members[0].ID || members[i].OperationKey != members[0].OperationKey {
					t.Fatal("duplicate membership or operation key")
				}
			}
			m := members[0]
			_, err := f.pool.Exec(f.ctx, `INSERT INTO musicgetter.destination_memberships(owner_id,connection_id,collection_id,destination_track_id) VALUES ($1,$2,$3,$4)`, f.user.ID, f.connection.ID, collection.ID, track.ID)
			requireSQLState(t, err, "23505")
			_, err = repo.Transition(f.ctx, f.user.ID, m.ID, domain.MembershipReserved, domain.MembershipUnknown)
			requireOK(t, err)
			replay, err := repo.Reserve(f.ctx, f.user.ID, f.connection.ID, collection.ID, track.ID)
			requireOK(t, err)
			if replay.State != domain.MembershipUnknown || replay.OperationKey != m.OperationKey {
				t.Fatal("retry erased uncertain outcome")
			}
			_, err = repo.Transition(f.ctx, f.user.ID, m.ID, domain.MembershipReserved, domain.MembershipApplied)
			if !errors.Is(err, domain.ErrConflict) {
				t.Fatal("stale membership transition succeeded")
			}
			_, err = repo.Transition(f.ctx, f.user.ID, m.ID, domain.MembershipUnknown, domain.MembershipApplied)
			requireOK(t, err)
		})
	}
}

func requireSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var e *pgconn.PgError
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("expected SQLSTATE %s, got %v", code, err)
	}
}
