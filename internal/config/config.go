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
	// Groq's free tier counts input + reserved output toward a 12K TPM cap;
	// with ~4K reserved for the JSON response and ~1K of system prompt, 5000
	// for the article body keeps total request tokens at ~10K with headroom
	// for known-words list and a schema-retry attempt. Paid-tier deployments
	// can raise this value via env.
	MaxTokensPerArticle int `env:"MAX_TOKENS_PER_ARTICLE" envDefault:"5000"`
	// AdminUserID is the single Telegram user id allowed to invoke admin
	// commands (and that receives the startup ping). Zero — the default —
	// disables all admin functionality entirely.
	AdminUserID int64 `env:"ADMIN_USER_ID" envDefault:"0"`
	// YandexDictAPIKey enables Yandex Dictionary lookups for word translations.
	// When empty the LLM-generated translation is used as-is.
	YandexDictAPIKey string `env:"YANDEX_DICT_API_KEY"`
	// QuizDailyGoal is the number of correct quiz answers per day that
	// triggers the bonus XP payout. Reset at the day boundary.
	QuizDailyGoal int `env:"QUIZ_DAILY_GOAL" envDefault:"20"`
	// QuizXPPerCorrect is the XP awarded for a single correct answer.
	QuizXPPerCorrect int `env:"QUIZ_XP_PER_CORRECT" envDefault:"10"`
	// QuizXPBonusGoal is the bonus XP added once per day when the user
	// reaches QuizDailyGoal correct answers.
	QuizXPBonusGoal int `env:"QUIZ_XP_BONUS_GOAL" envDefault:"50"`
	// QuizPollEnabled controls whether quiz cards can use the native
	// Telegram poll UI. When false all cards use inline buttons only.
	QuizPollEnabled bool `env:"QUIZ_POLL_ENABLED" envDefault:"true"`
	// VocabTargetWords is the target number of new vocabulary words to
	// extract per article. For long articles a separate vocab-only LLM
	// pass runs over the full text to reach this target.
	VocabTargetWords int `env:"VOCAB_TARGET_WORDS" envDefault:"20"`
}

func Load() (*Config, error) {
	_ = godotenv.Load() // best-effort
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
