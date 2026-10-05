// Package app owns process lifecycle; business use cases live elsewhere.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"musicgetter/internal/config"
)

func NewServer(cfg config.Config, handler http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr: cfg.HTTPAddr, Handler: handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout, ReadTimeout: cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout,
		MaxHeaderBytes: 16 << 10,
		ErrorLog:       slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

// Serve preserves request contexts during normal draining. Only an exhausted
// shutdown deadline cancels active requests and forcibly closes connections.
func Serve(ctx context.Context, server *http.Server, listener net.Listener, shutdownTimeout time.Duration,
	drain func(), logger *slog.Logger) error {
	defer server.Close()
	requestCtx, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	server.BaseContext = func(net.Listener) context.Context { return requestCtx }
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	logger.InfoContext(ctx, "http_listening", "address", listener.Addr().String())
	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("HTTP server failed")
	case <-ctx.Done():
	}
	drain()
	logger.Info("shutdown_started")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := server.Shutdown(shutdownCtx)
	if err != nil {
		cancelRequests()
		_ = server.Close()
	}
	serveErr := <-served
	if err != nil {
		return errors.New("HTTP shutdown deadline exceeded")
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.New("HTTP server failed")
	}
	logger.Info("shutdown_completed")
	return nil
}
