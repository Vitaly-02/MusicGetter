package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"musicgetter/internal/logging"
)

type requestIDKey struct{}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// Middleware propagates deadlines and correlation through request.Context().
// Handlers and downstream clients must honor that context.
func Middleware(logger *slog.Logger, timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID(id) {
			var bytes [16]byte
			_, _ = rand.Read(bytes[:])
			id = hex.EncodeToString(bytes[:])
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		ctx = context.WithValue(ctx, requestIDKey{}, id)
		logger := logger.With("request_id", id)
		ctx = logging.WithContext(ctx, logger)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", id)
		recorder := &responseWriter{ResponseWriter: w}
		start := time.Now()
		defer func() {
			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			// No URL, query string, headers or panic value: these can contain secrets.
			logger.InfoContext(ctx, "http_request", "method", r.Method, "status", status,
				"duration_ms", time.Since(start).Milliseconds(), "aborted", recorder.aborted)
		}()
		defer func() {
			if recovered := recover(); recovered != nil {
				recorder.aborted = true
				if recovered == http.ErrAbortHandler {
					panic(http.ErrAbortHandler)
				}
				logger.ErrorContext(ctx, "http_panic", "code", "internal_error")
				if recorder.status != 0 {
					// A partially written response cannot safely be replaced by JSON.
					panic(http.ErrAbortHandler)
				}
				writeError(recorder, r, http.StatusInternalServerError, "internal_error", "Internal server error")
			}
		}()
		next.ServeHTTP(recorder, r)
	})
}

func validRequestID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

type responseWriter struct {
	http.ResponseWriter
	status  int
	aborted bool
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 200 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *responseWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Flush() { _ = w.FlushError() }

// Unwrap preserves http.ResponseController functionality for future handlers.
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
