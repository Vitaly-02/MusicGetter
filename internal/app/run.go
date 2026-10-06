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
	uploads := postgres.NewUploadRepository(pool)
	extensionAPI := api.NewExtensionAPI(pairing.New(postgres.NewSessionRepository(pool)), uploads, uploads, api.ExtensionPolicy{Origins: cfg.ExtensionOrigins, IPPerMinute: cfg.APIIPPerMinute, OwnerPerMinute: cfg.APIOwnerPerMinute, ClaimPerMinute: cfg.APIClaimPerMinute})
	handler := api.NewHandler(logger, health, cfg.RequestTimeout, extensionAPI.Register)
	server := NewServer(cfg, handler, logger)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return errors.New("HTTP listener failed")
	}
	return Serve(ctx, server, listener, cfg.ShutdownTimeout, health.Drain, logger)
}
