//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"musicgetter/internal/config"
	"musicgetter/internal/domain"
	"musicgetter/internal/storage/postgres"
	"musicgetter/migrations"
)

type fixture struct {
	ctx         context.Context
	cfg         config.Database
	pool        *pgxpool.Pool
	user        domain.User
	profile     domain.SourceProfile
	source      domain.SourceCollection
	connection  domain.DestinationConnection
	destination domain.DestinationCollection
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	f := fixture{cfg: isolatedDatabase(t)}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	f.ctx = ctx
	requireOK(t, postgres.Migrate(ctx, f.cfg, migrations.FS, "up"))
	pool, err := postgres.Open(ctx, f.cfg)
	requireOK(t, err)
	t.Cleanup(pool.Close)
	f.pool = pool
	f.user, err = postgres.NewUserRepository(pool).EnsureTelegram(ctx, 42)
	requireOK(t, err)
	sr := postgres.NewSourceRepository(pool)
	f.profile, err = sr.EnsureProfile(ctx, domain.SourceProfile{OwnerID: f.user.ID, Source: domain.SourceSpotify, ProfileKey: "main", Label: "Main"})
	requireOK(t, err)
	f.source, err = sr.EnsureCollection(ctx, domain.SourceCollection{OwnerID: f.user.ID, ProfileID: f.profile.ID, Source: f.profile.Source, CollectionKey: "liked", Kind: domain.SourceFavorites, Title: "Liked songs"})
	requireOK(t, err)
	dr := postgres.NewDestinationCollectionRepository(pool)
	f.connection, err = dr.EnsureConnection(ctx, domain.DestinationConnection{OwnerID: f.user.ID, Adapter: "fake", AccountKey: "main"})
	requireOK(t, err)
	f.destination, err = dr.Ensure(ctx, domain.DestinationCollection{OwnerID: f.user.ID, ConnectionID: f.connection.ID, ExternalKey: "favorites", Kind: domain.CollectionFavorites, Title: "Favorites"})
	requireOK(t, err)
	return f
}
func requireOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func (f fixture) track(t *testing.T, title string, key *string) domain.CanonicalTrack {
	t.Helper()
	track, err := postgres.NewCanonicalTrackRepository(f.pool).Ensure(f.ctx, domain.CanonicalTrackInput{OwnerID: f.user.ID, ProfileID: f.profile.ID, Source: f.profile.Source, SourceTrackKey: key, Metadata: domain.TrackMetadata{Title: title, Artists: []string{"Artist"}, Album: "Album"}})
	requireOK(t, err)
	return track
}
func (f fixture) remote(t *testing.T, key string) domain.DestinationTrack {
	t.Helper()
	track, err := postgres.NewDestinationTrackRepository(f.pool).Ensure(f.ctx, domain.DestinationTrack{OwnerID: f.user.ID, ConnectionID: f.connection.ID, ExternalKey: key, Metadata: domain.TrackMetadata{Title: "Song", Artists: []string{"Artist"}}})
	requireOK(t, err)
	return track
}
func (f fixture) importRecord(t *testing.T, key string) domain.Import {
	t.Helper()
	record, err := postgres.NewImportRepository(f.pool).Create(f.ctx, f.user.ID, f.source.ID, f.destination.ID, key)
	requireOK(t, err)
	return record
}
func (f fixture) item(t *testing.T, record domain.Import, title string) domain.ImportItem {
	t.Helper()
	track := f.track(t, title, nil)
	item, err := postgres.NewImportRepository(f.pool).AddItem(f.ctx, f.user.ID, record.ID, track.ID, 0)
	requireOK(t, err)
	return item
}
func (f fixture) enqueue(t *testing.T, item domain.ImportItem, maxAttempts int) domain.ImportJob {
	t.Helper()
	job, err := postgres.NewJobRepository(f.pool).Enqueue(f.ctx, domain.ImportJob{OwnerID: f.user.ID, ImportID: item.ImportID, ItemID: item.ID, Kind: domain.JobMatch, LogicalKey: string(item.ID), MaxAttempts: maxAttempts})
	requireOK(t, err)
	return job
}
