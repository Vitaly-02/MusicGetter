package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"musicgetter/internal/logging"
)

func TestRecoveryDoesNotReplaceFlushedResponse(t *testing.T) {
	handler := Middleware(logging.New(io.Discard, slog.LevelInfo), time.Second,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Fatal(err)
			}
			panic("after flush")
		}))
	w := httptest.NewRecorder()
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Error("flushed response was not aborted")
		}
		if w.Code != 200 || w.Body.Len() != 0 {
			t.Error("flushed response was replaced")
		}
	}()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
}
