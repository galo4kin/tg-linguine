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
	// MaxArticleSizeKB caps the raw HTTP response body in KB. 4096 covers
	// long-form pages like Wikipedia featured articles whose unminified HTML
	// can run 1–3 MB; the cap still protects against accidental multi-MB
	// downloads of non-article assets.
	MaxArticleSizeKB int   `env:"MAX_ARTICLE_SIZE_KB" envDefault:"4096"`
	GroqModel       string `env:"GROQ_MODEL"         envDefault:"llama-3.3-70b-versatile"`
	LogLevel        string `env:"LOG_LEVEL"          envDefault:"info"`
	LogMaxSizeMB    int    `env:"LOG_MAX_SIZE_MB"    envDefault:"10"`
	LogMaxBackups   int    `env:"LOG_MAX_BACKUPS"    envDefault:"5"`
	LogMaxAgeDays   int    `env:"LOG_MAX_AGE_DAYS"   envDefault:"30"`
	LogStdout       bool   `env:"LOG_STDOUT"         envDefault:"false"`
	RateLimitPerHour int   `env:"RATE_LIMIT_PER_HOUR" envDefault:"10"`
	// MaxTokensPerArticle is the per-article extracted-text budget (estimated
	// tokens) before we ask the LLM to fall back to truncation or pre-summary.
	// While llama-3.3-70b-versatile claims a 128K context, Groq's free tier
	// rejects requests well below that with HTTP 413 (~10–12K tokens of input).
	// 10000 is a safe default for free-tier accounts; bump this on paid tiers
	// where the per-request input cap is higher.
	MaxTokensPerArticle int `env:"MAX_TOKENS_PER_ARTICLE" envDefault:"10000"`
	// AdminUserID is the single Telegram user id allowed to invoke admin
	// commands (and that receives the startup ping). Zero — the default —
	// disables all admin functionality entirely.
	AdminUserID int64 `env:"ADMIN_USER_ID" envDefault:"0"`
}

func Load() (*Config, error) {
	_ = godotenv.Load() // best-effort
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
