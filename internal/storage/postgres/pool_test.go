package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"musicgetter/internal/config"
)

func TestOpenRejectsMalformedURLWithoutSecrets(t *testing.T) {
	pool, err := Open(context.Background(), config.Database{URL: "postgres://user:secret@host/%zz"})
	if pool != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatalf("expected safe configuration error, got %v", err)
	}
}

func TestOpenHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool, err := Open(ctx, config.Database{URL: "postgres://127.0.0.1:1/test?sslmode=disable", MaxConns: 1, ConnectTimeout: time.Second, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute})
	if pool != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
