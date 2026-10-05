//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"musicgetter/internal/storage/postgres"
	"musicgetter/migrations"
)

func TestSchemaUpgradePreservesUsersAndAllDownMigrationsWork(t *testing.T) {
	cfg := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	initial := fstest.MapFS{}
	for _, name := range []string{"00001_bootstrap.sql", "00002_accounts_collections.sql"} {
		data, err := migrations.FS.ReadFile(name)
		requireOK(t, err)
		initial[name] = &fstest.MapFile{Data: data}
	}
	requireOK(t, postgres.Migrate(ctx, cfg, initial, "up"))
	pool, err := postgres.Open(ctx, cfg)
	requireOK(t, err)
	defer pool.Close()
	user, err := postgres.NewUserRepository(pool).EnsureTelegram(ctx, 987)
	requireOK(t, err)
	requireOK(t, postgres.Migrate(ctx, cfg, migrations.FS, "up"))
	persisted, err := postgres.NewUserRepository(pool).Get(ctx, user.ID)
	requireOK(t, err)
	if persisted.TelegramUserID != 987 {
		t.Fatal("upgrade lost user")
	}
	requireOK(t, postgres.Ready(ctx, pool))
	for version := migrations.Version; version > 0; version-- {
		requireOK(t, postgres.Migrate(ctx, cfg, migrations.FS, "down"))
		var actual int64
		requireOK(t, pool.QueryRow(ctx, `SELECT COALESCE(max(version_id),0) FROM public.goose_db_version`).Scan(&actual))
		if actual != version-1 {
			t.Fatalf("down from %d left version %d", version, actual)
		}
	}
	var absent bool
	requireOK(t, pool.QueryRow(ctx, `SELECT to_regnamespace('musicgetter') IS NULL`).Scan(&absent))
	if !absent {
		t.Fatal("schema survived full rollback")
	}
	requireOK(t, postgres.Migrate(ctx, cfg, migrations.FS, "up"))
	requireOK(t, postgres.Ready(ctx, pool))
}
