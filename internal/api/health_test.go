package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"musicgetter/internal/logging"
)

func TestHealthRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		dbErr              error
		drain              bool
		want               int
	}{
		{"live ignores database failure", "GET", "/health/live", errors.New("secret DSN"), false, 200},
		{"ready", "GET", "/health/ready", nil, false, 200},
		{"unavailable", "GET", "/health/ready", errors.New("secret DSN"), false, 503},
		{"draining", "GET", "/health/ready", nil, true, 503},
		{"live while draining", "GET", "/health/live", nil, true, 200},
		{"wrong method", "POST", "/health/live", nil, false, 405},
		{"not found", "GET", "/missing", nil, false, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			calls := 0
			health := NewHealth(func(context.Context) error { calls++; return tc.dbErr }, time.Second)
			if tc.drain {
				health.Drain()
			}
			handler := NewHandler(logging.New(&logs, slog.LevelDebug), health, time.Second)
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.Header.Set("X-Request-ID", "test-request")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d", w.Code, tc.want)
			}
			if w.Header().Get("X-Request-ID") != "test-request" || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
				t.Fatal("missing response headers")
			}
			if tc.path != "/health/ready" || tc.drain {
				if calls != 0 {
					t.Fatal("unnecessary database check")
				}
			}
			if strings.Contains(w.Body.String()+logs.String(), "secret DSN") {
				t.Fatal("dependency details leaked")
			}
			if tc.want >= 400 {
				var response errorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.RequestID != "test-request" || response.Error.Code == "" {
					t.Fatal("missing error envelope")
				}
			}
			if tc.want == 405 && w.Header().Get("Allow") != "GET" {
				t.Fatal("missing Allow")
			}
		})
	}
}

func TestReadinessDeadlineAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		health := NewHealth(func(ctx context.Context) error {
			if _, ok := ctx.Deadline(); !ok {
				t.Error("missing deadline")
			}
			if RequestID(ctx) == "" {
				t.Error("missing propagated request ID")
			}
			<-ctx.Done()
			return ctx.Err()
		}, 20*time.Millisecond)
		handler := NewHandler(logging.New(io.Discard, slog.LevelInfo), health, time.Second)
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/health/ready", nil).WithContext(ctx))
		cancel()
		if w.Code != 503 {
			t.Fatalf("got %d", w.Code)
		}
	}
}

func TestDrainDuringReadinessCheck(t *testing.T) {
	var health *Health
	health = NewHealth(func(context.Context) error { health.Drain(); return nil }, time.Second)
	w := httptest.NewRecorder()
	health.Ready(w, httptest.NewRequest("GET", "/health/ready", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", w.Code)
	}
}
