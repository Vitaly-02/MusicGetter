package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"musicgetter/internal/logging"
)

type Health struct {
	check    func(context.Context) error
	timeout  time.Duration
	draining atomic.Bool
}

func NewHealth(check func(context.Context) error, timeout time.Duration) *Health {
	return &Health{check: check, timeout: timeout}
}

func (h *Health) Drain() { h.draining.Store(true) }

func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	if h.draining.Load() {
		writeError(w, r, http.StatusServiceUnavailable, "shutting_down", "Service is shutting down")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	if err := h.check(ctx); err != nil {
		logging.FromContext(ctx).WarnContext(ctx, "readiness_failed", "code", "dependency_unavailable")
		writeError(w, r, http.StatusServiceUnavailable, "not_ready", "Service is not ready")
		return
	}
	// Shutdown may have started while the dependency check was running.
	if h.draining.Load() {
		writeError(w, r, http.StatusServiceUnavailable, "shutting_down", "Service is shutting down")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func NewHandler(logger *slog.Logger, health *Health, requestTimeout time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", getOnly(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "live"})
	}))
	mux.HandleFunc("/health/ready", getOnly(health.Ready))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not_found", "Route not found")
	})
	return Middleware(logger, requestTimeout, mux)
}

func getOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
			return
		}
		next(w, r)
	}
}
