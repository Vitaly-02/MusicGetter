package app

import (
	"context"
	"errors"
	"log/slog"
	"net"

	"musicgetter/internal/api"
	"musicgetter/internal/config"
	"musicgetter/internal/pairing"
	"musicgetter/internal/storage/postgres"
)

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	pool, err := postgres.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	health := api.NewHealth(func(ctx context.Context) error { return postgres.Ready(ctx, pool) }, cfg.HealthTimeout)
	handler := api.NewHandler(logger, health, cfg.RequestTimeout, api.ExtensionRoutes(pairing.New(postgres.NewSessionRepository(pool))))
	server := NewServer(cfg, handler, logger)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return errors.New("HTTP listener failed")
	}
	return Serve(ctx, server, listener, cfg.ShutdownTimeout, health.Drain, logger)
}
