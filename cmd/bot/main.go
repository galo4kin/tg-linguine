package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/config"
	"github.com/nikita/tg-linguine/internal/crypto"
	"github.com/nikita/tg-linguine/internal/dictionary"
	"github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/llm/groq"
	"github.com/nikita/tg-linguine/internal/logger"
	"github.com/nikita/tg-linguine/internal/storage"
	"github.com/nikita/tg-linguine/internal/telegram"
	"github.com/nikita/tg-linguine/internal/users"
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

	cipher, err := crypto.NewFromBase64(cfg.EncryptionKey)
	if err != nil {
		log.Error("crypto init", "err", err)
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

	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	apiKeys := users.NewSQLiteAPIKeyRepository(db, cipher)

	groqClient := groq.New(
		groq.WithHTTPClient(&http.Client{
			Timeout: time.Duration(cfg.HTTPTimeoutSec) * time.Second,
		}),
		groq.WithModel(cfg.GroqModel),
	)

	extractor := articles.NewReadabilityExtractor(
		time.Duration(cfg.HTTPTimeoutSec)*time.Second,
		int64(cfg.MaxArticleSizeKB)<<10,
	)
	articleRepo := articles.NewSQLiteRepository(db)
	dictRepo := dictionary.NewSQLiteRepository(db)
	articleWordsRepo := dictionary.NewSQLiteArticleWordsRepository(db)
	statusRepo := dictionary.NewSQLiteUserWordStatusRepository(db)

	articleSvc := articles.NewService(articles.ServiceDeps{
		DB:           db,
		Users:        usersSvc,
		Languages:    langs,
		Keys:         apiKeys,
		Extractor:    extractor,
		LLM:          groqClient,
		Articles:     articleRepo,
		Dictionary:   dictRepo,
		ArticleWords: articleWordsRepo,
		Statuses:     statusRepo,
		Log:          log,
	})

	tgBot, err := telegram.New(cfg, log, telegram.Deps{
		Bundle:      bundle,
		Users:       usersSvc,
		Languages:   langs,
		APIKeys:     apiKeys,
		LLMProvider: groqClient,
		Articles:    articleSvc,
	})
	if err != nil {
		log.Error("telegram init", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	tgBot.Start(ctx)
	log.Info("shutdown")
}
