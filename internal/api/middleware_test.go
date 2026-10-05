package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"musicgetter/internal/logging"
)

func TestRequestIDAndContext(t *testing.T) {
	type key struct{}
	for _, input := range []string{"", "valid-Request_42", "bad request", strings.Repeat("x", 65)} {
		var logs bytes.Buffer
		handler := Middleware(logging.New(&logs, slog.LevelInfo), time.Second, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(key{}) != "parent" {
				t.Error("lost parent value")
			}
			id := RequestID(r.Context())
			if !validRequestID(id) {
				t.Error("invalid generated ID")
			}
			if validRequestID(input) && id != input {
				t.Error("lost incoming ID")
			}
			if !validRequestID(input) && id == input {
				t.Error("accepted invalid ID")
			}
			deadline, ok := r.Context().Deadline()
			if !ok || time.Until(deadline) > time.Second {
				t.Error("missing deadline")
			}
			logging.FromContext(r.Context()).InfoContext(r.Context(), "downstream")
			w.WriteHeader(204)
		}))
		r := httptest.NewRequest("GET", "/?secret-token=hidden", nil)
		r.Header.Set("X-Request-ID", input)
		r = r.WithContext(context.WithValue(r.Context(), key{}, "parent"))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 204 || w.Header().Get("X-Request-ID") == "" {
			t.Fatal("missing ID response")
		}
		if strings.Contains(logs.String(), "secret-token") {
			t.Fatal("query logged")
		}
		if strings.Count(logs.String(), w.Header().Get("X-Request-ID")) != 2 {
			t.Fatal("logger did not propagate ID")
		}
	}
}

func TestRecoveryHidesPanic(t *testing.T) {
	var logs bytes.Buffer
	handler := Middleware(logging.New(&logs, slog.LevelInfo), time.Second, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("private credential") }))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 || !strings.Contains(w.Body.String(), "internal_error") {
		t.Fatalf("unexpected recovery: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(logs.String()+w.Body.String(), "private credential") {
		t.Fatal("panic leaked")
	}
}

func TestRecoveryAbortsPartialResponse(t *testing.T) {
	handler := Middleware(logging.New(io.Discard, slog.LevelInfo), time.Second, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial"))
		panic("do not append JSON")
	}))
	w := httptest.NewRecorder()
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Error("partial response was not aborted")
		}
		if w.Body.String() != "partial" {
			t.Error("mixed success and error body")
		}
	}()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
}

func TestEarlierParentDeadlineIsPreserved(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	expected, _ := ctx.Deadline()
	handler := Middleware(logging.New(io.Discard, slog.LevelInfo), time.Minute, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		actual, _ := r.Context().Deadline()
		if !actual.Equal(expected) {
			t.Error("extended parent deadline")
		}
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil).WithContext(ctx))
}
