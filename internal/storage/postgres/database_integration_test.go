//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"musicgetter/internal/config"
)

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
