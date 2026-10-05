//go:build integration

package postgres_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"musicgetter/internal/api"
	"musicgetter/internal/logging"
	"musicgetter/internal/storage/postgres"
	"musicgetter/migrations"
)

func TestBootstrapAgainstPostgres(t *testing.T) {
	cfg := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := postgres.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if pool.Config().MaxConns != cfg.MaxConns {
		t.Fatal("pool configuration ignored")
	}
	health := api.NewHealth(func(ctx context.Context) error { return postgres.Ready(ctx, pool) }, time.Second)
	handler := api.NewHandler(logging.New(io.Discard, slog.LevelInfo), health, 2*time.Second)
	check := func(path string, want int) {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Fatalf("%s: got %d, want %d", path, w.Code, want)
		}
	}
	check("/health/live", 200)
	check("/health/ready", 503)
	// Separate runners must serialize; both up operations should succeed.
	var workers sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		workers.Go(func() { errs <- postgres.Migrate(ctx, cfg, migrations.FS, "up") })
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	check("/health/ready", 200)
	if err := postgres.Migrate(ctx, cfg, migrations.FS, "up"); err != nil {
		t.Fatal("up replay:", err)
	}
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = pool.Exec(short, "SELECT pg_sleep(10)")
	stop()
	if err == nil {
		t.Fatal("SQL ignored deadline")
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatal("pool failed after cancellation:", err)
	}
	if err := postgres.Migrate(ctx, cfg, migrations.FS, "down"); err != nil {
		t.Fatal(err)
	}
	check("/health/ready", 503)
	if err := postgres.Migrate(ctx, cfg, migrations.FS, "up"); err != nil {
		t.Fatal(err)
	}
	check("/health/ready", 200)
	pool.Close()
	check("/health/live", 200)
	check("/health/ready", 503)
}

func TestFailedMigrationRollsBack(t *testing.T) {
	cfg := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := postgres.Migrate(ctx, cfg, migrations.FS, "up"); err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{}
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := migrations.FS.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = &fstest.MapFile{Data: data}
	}
	files["00005_failure.sql"] = &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE musicgetter.rollback_probe (id int);\nSELECT missing_migration_function();\n-- +goose Down\nDROP TABLE musicgetter.rollback_probe;\n")}
	if err := postgres.Migrate(ctx, cfg, files, "up"); err == nil {
		t.Fatal("expected migration failure")
	}
	pool, err := postgres.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var absent bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('musicgetter.rollback_probe') IS NULL").Scan(&absent); err != nil || !absent {
		t.Fatalf("failed DDL not rolled back: absent=%t err=%v", absent, err)
	}
	if err := postgres.Ready(ctx, pool); err != nil {
		t.Fatal("version changed after rollback:", err)
	}
}

func TestBootstrapDownRestrictsUnknownObjects(t *testing.T) {
	cfg := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base, err := migrations.FS.ReadFile("00001_bootstrap.sql")
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"00001_bootstrap.sql": &fstest.MapFile{Data: base}}
	if err := postgres.Migrate(ctx, cfg, files, "up"); err != nil {
		t.Fatal(err)
	}
	pool, err := postgres.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, "CREATE TABLE musicgetter.preserve_me (id int)"); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, cfg, files, "down"); err == nil {
		t.Fatal("unsafe schema drop succeeded")
	}
	var version int
	if err := pool.QueryRow(ctx, "SELECT max(version_id) FROM public.goose_db_version").Scan(&version); err != nil || version != 1 {
		t.Fatal("failed down changed version", version, err)
	}
}
