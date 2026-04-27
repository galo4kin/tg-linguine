package config

import (
	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Config struct {
	BotToken        string `env:"BOT_TOKEN,required"`
	DBPath          string `env:"DB_PATH"            envDefault:"./bot.db"`
	LogPath         string `env:"LOG_PATH"           envDefault:"./bot.log"`
	EncryptionKey   string `env:"ENCRYPTION_KEY,required"`
	HTTPTimeoutSec  int    `env:"HTTP_TIMEOUT_SEC"   envDefault:"20"`
	MaxArticleSizeKB int   `env:"MAX_ARTICLE_SIZE_KB" envDefault:"512"`
	LogLevel        string `env:"LOG_LEVEL"          envDefault:"info"`
	LogMaxSizeMB    int    `env:"LOG_MAX_SIZE_MB"    envDefault:"10"`
	LogMaxBackups   int    `env:"LOG_MAX_BACKUPS"    envDefault:"5"`
	LogMaxAgeDays   int    `env:"LOG_MAX_AGE_DAYS"   envDefault:"30"`
	LogStdout       bool   `env:"LOG_STDOUT"         envDefault:"false"`
}

func Load() (*Config, error) {
	_ = godotenv.Load() // best-effort
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
