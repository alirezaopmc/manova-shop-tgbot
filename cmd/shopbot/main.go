package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mymmrac/telego"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/telegram"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("starting manova-shop-tgbot daemon")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := memory.NewStore()

	token := cfg.TelegramBotToken.Expose()
	if token == "" {
		slog.Warn("TELEGRAM_BOT_TOKEN is empty. Set TELEGRAM_BOT_TOKEN in .env to connect to Telegram live.")
		fmt.Println("----------------------------------------------------------------------")
		fmt.Println("manova-shop-tgbot initialized successfully in standby mode.")
		fmt.Println("To connect live:")
		fmt.Println("1. Set TELEGRAM_BOT_TOKEN=<your_bot_token> in .env or environment")
		fmt.Println("2. Re-run: go run ./cmd/shopbot")
		fmt.Println("----------------------------------------------------------------------")
		<-ctx.Done()
		slog.Info("standby daemon shutting down")
		return
	}

	bot, err := telego.NewBot(token, telego.WithDiscardLogger())
	if err != nil {
		slog.Error("failed to initialize telegram bot", "error", err)
		os.Exit(1)
	}

	service := telegram.NewService(bot, cfg, store)

	go func() {
		if err := service.Start(ctx); err != nil && err != context.Canceled {
			slog.Error("telegram service stopped with error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("graceful shutdown complete")
}
