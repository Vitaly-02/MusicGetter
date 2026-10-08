package main

import (
	"context"
	"log/slog"
	"musicgetter/internal/config"
	importer "musicgetter/internal/import"
	"musicgetter/internal/logging"
	"musicgetter/internal/matcher"
	"musicgetter/internal/storage/postgres"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() { os.Exit(run()) }
func run() int {
	logger := logging.New(os.Stdout, slog.LevelInfo).With("component", "import-worker")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := start(ctx, logger); err != nil && ctx.Err() == nil {
		logger.Error("worker_failed", "error", err)
		return 1
	}
	return 0
}
func start(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	options, err := config.LoadWorker()
	if err != nil {
		return err
	}
	logger = logging.New(os.Stdout, cfg.LogLevel).With("component", "import-worker")
	pool, err := postgres.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = postgres.Ready(startup, pool); err != nil {
		return err
	}
	store := postgres.NewPipelineRepository(pool)
	// Register real, contract-tested destination factories here when selected.
	// Empty registry fails closed per item; no demo destination in production.
	registry := importer.Registry{}
	logger.WarnContext(ctx, "no_destination_adapters_registered")
	searchOptions, err := config.LoadMatcher()
	if err != nil {
		return err
	}
	engine, err := matcher.NewEngine(searchOptions)
	if err != nil {
		return err
	}
	worker := importer.Worker{Queue: postgres.NewJobRepository(pool), Repairer: store, Pipeline: &importer.Pipeline{Store: store, Resolver: registry, Matcher: engine}, Options: options, Logger: logger}
	logger.InfoContext(ctx, "worker_started", "concurrency", options.Concurrency)
	return worker.Run(ctx)
}
