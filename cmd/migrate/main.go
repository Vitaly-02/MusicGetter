package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"musicgetter/internal/config"
	"musicgetter/internal/logging"
	"musicgetter/internal/storage/postgres"
	"musicgetter/migrations"
)

func main() { os.Exit(run()) }

func run() int {
	logger := logging.New(os.Stdout, slog.LevelInfo)
	if len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "down") {
		logger.Error("usage: migrate up|down")
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration_failed", "error", err)
		return 1
	}
	logger = logging.New(os.Stdout, cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, cfg.MigrationTimeout)
	defer cancel()
	if err := postgres.Migrate(ctx, cfg.Database, migrations.FS, os.Args[1]); err != nil {
		logger.Error("migration_failed", "error", err)
		return 1
	}
	logger.Info("migration_completed", "direction", os.Args[1])
	return 0
}
