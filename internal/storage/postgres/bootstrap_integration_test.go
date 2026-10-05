//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"musicgetter/internal/api"
	"musicgetter/internal/config"
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
	base, err := migrations.FS.ReadFile("00001_bootstrap.sql")
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{
		"00001_bootstrap.sql": &fstest.MapFile{Data: base},
		"00002_failure.sql":   &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE musicgetter.rollback_probe (id int);\nSELECT missing_migration_function();\n-- +goose Down\nDROP TABLE musicgetter.rollback_probe;\n")},
	}
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
	// Future contents must not be silently dropped by the bootstrap down migration.
	if _, err := pool.Exec(ctx, "CREATE TABLE musicgetter.preserve_me (id int)"); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, cfg, migrations.FS, "down"); err == nil {
		t.Fatal("unsafe schema drop succeeded")
	}
	if err := postgres.Ready(ctx, pool); err != nil {
		t.Fatal("failed down changed version:", err)
	}
}

// Tests create and drop only randomly named databases, never the supplied database.
// The test role needs CREATEDB; explicit opt-in avoids accidental production use.
func isolatedDatabase(t *testing.T) config.Database {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is required with -tags=integration (use a local role with CREATEDB)")
	}
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := pgx.ConnectConfig(ctx, parsed)
	if err != nil {
		t.Fatal("cannot connect to integration database")
	}
	var random [8]byte
	_, _ = rand.Read(random[:])
	name := "musicgetter_test_" + hex.EncodeToString(random[:])
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		_ = admin.Close(ctx)
		t.Fatal("cannot create integration database (CREATEDB required)")
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		defer admin.Close(cleanupCtx)
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE "+identifier+" WITH (FORCE)"); err != nil {
			t.Error("cannot clean up integration database", name)
		}
	})
	// ConnConfig.ConnString returns the ORIGINAL string, not edited fields.
	// Rewrite the DSN explicitly and verify it before returning it to migrations.
	isolatedDSN := dsn + " dbname=" + name
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal("invalid integration database URL")
		}
		u.Path, u.RawPath = "/"+name, ""
		query := u.Query()
		query.Del("dbname")
		query.Del("database")
		u.RawQuery = query.Encode()
		isolatedDSN = u.String()
	}
	probe, err := pgx.Connect(ctx, isolatedDSN)
	if err != nil {
		t.Fatal("cannot connect to isolated database")
	}
	defer probe.Close(ctx)
	var actual string
	if err := probe.QueryRow(ctx, "SELECT current_database()").Scan(&actual); err != nil || actual != name {
		t.Fatal("integration database isolation failed")
	}
	return config.Database{URL: isolatedDSN, MaxConns: 3, ConnectTimeout: 3 * time.Second, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute}
}
