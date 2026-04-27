package logger

import (
	"io"
	"log/slog"
	"os"

	"github.com/nikita/tg-linguine/internal/config"
	"gopkg.in/natefinch/lumberjack.v2"
)

func New(cfg *config.Config) *slog.Logger {
	rotator := &lumberjack.Logger{
		Filename:   cfg.LogPath,
		MaxSize:    cfg.LogMaxSizeMB,
		MaxBackups: cfg.LogMaxBackups,
		MaxAge:     cfg.LogMaxAgeDays,
		Compress:   true,
	}

	var w io.Writer = rotator
	if cfg.LogStdout {
		w = io.MultiWriter(rotator, os.Stdout)
	}

	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}
