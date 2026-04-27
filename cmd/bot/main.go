package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nikita/tg-linguine/internal/config"
	"github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/logger"
	"github.com/nikita/tg-linguine/internal/storage"
	"github.com/nikita/tg-linguine/internal/telegram"
)

var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}

	log := logger.New(cfg)
	log.Info("boot", "version", version)

	bundle, err := i18n.NewBundle()
	if err != nil {
		log.Error("i18n bundle", "err", err)
		os.Exit(1)
	}

	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		log.Error("open db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := storage.RunMigrations(db, log); err != nil {
		log.Error("migrations", "err", err)
		os.Exit(1)
	}

	tgBot, err := telegram.New(cfg, log, bundle)
	if err != nil {
		log.Error("telegram init", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	tgBot.Start(ctx)
	log.Info("shutdown")
}
