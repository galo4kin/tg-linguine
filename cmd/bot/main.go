package main

import (
	"fmt"
	"os"

	"github.com/nikita/tg-linguine/internal/config"
	"github.com/nikita/tg-linguine/internal/logger"
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
}
