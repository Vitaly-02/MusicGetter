package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"musicgetter/internal/config"
	"musicgetter/internal/logging"
	"musicgetter/internal/pairing"
	"musicgetter/internal/storage/postgres"
	"musicgetter/internal/telegram"
)

func main() { os.Exit(run()) }
func run() int {
	logger := logging.New(os.Stdout, slog.LevelInfo).With("component", "telegram-bot")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := start(ctx, logger); err != nil && ctx.Err() == nil {
		logger.Error("bot_failed", "error", err)
		return 1
	}
	return 0
}
func start(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger = logging.New(os.Stdout, cfg.LogLevel).With("component", "telegram-bot")
	client, err := telegram.NewClient(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if err != nil {
		return err
	}
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
	me, err := client.GetMe(startup)
	if err != nil {
		return err
	}
	if me.ID <= 0 || !me.IsBot || me.Username == "" {
		return errors.New("invalid Telegram bot identity")
	}
	conn, err := pgx.ConnectConfig(startup, pool.Config().ConnConfig.Copy())
	if err != nil {
		return errors.New("bot lock connection failed")
	}
	defer func() {
		closing, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Close(closing)
	}()
	if err = telegram.AcquirePollLock(startup, conn, me.ID); err != nil {
		return err
	}
	if err = client.SetCommands(startup); err != nil {
		return err
	}
	queries := postgres.NewBotQueries(pool)
	handler := telegram.NewHandler(client, postgres.NewUserRepository(pool), pairing.New(postgres.NewSessionRepository(pool)), queries, me.Username)
	logger.InfoContext(ctx, "telegram_bot_started")
	return telegram.Poll(ctx, &telegram.LockedSource{Client: client, Connection: conn}, queries, handler, me.ID, logger)
}
