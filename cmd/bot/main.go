package main

import (
	"fmt"
	"os"

	"github.com/nikita/tg-linguine/internal/config"
	"github.com/nikita/tg-linguine/internal/logger"
	"github.com/nikita/tg-linguine/internal/storage"
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
}
