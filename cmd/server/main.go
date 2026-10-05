package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"musicgetter/internal/app"
	"musicgetter/internal/config"
	"musicgetter/internal/logging"
)

func main() { os.Exit(run()) }

func run() int {
	logger := logging.New(os.Stdout, slog.LevelInfo)
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration_failed", "error", err)
		return 1
	}
	logger = logging.New(os.Stdout, cfg.LogLevel)
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, cfg, logger); err != nil {
		logger.Error("server_failed", "error", err)
		return 1
	}
	return 0
}
