package telegram

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/config"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/telegram/handlers"
	"github.com/nikita/tg-linguine/internal/users"
)

type Bot struct {
	b      *bot.Bot
	log    *slog.Logger
	bundle *goi18n.Bundle
}

func New(cfg *config.Config, log *slog.Logger, bundle *goi18n.Bundle, usersSvc *users.Service) (*Bot, error) {
	tb := &Bot{log: log, bundle: bundle}

	opts := []bot.Option{
		bot.WithMiddlewares(tb.i18nMiddleware, tb.logMiddleware),
	}

	b, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		return nil, err
	}

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, handlers.Start(usersSvc, bundle, log))

	tb.b = b
	return tb, nil
}

func (tb *Bot) Start(ctx context.Context) {
	tb.b.Start(ctx)
}

func (tb *Bot) i18nMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		lang := "en"
		if update.Message != nil && update.Message.From != nil {
			lang = update.Message.From.LanguageCode
		}
		ctx = tgi18n.WithLocalizer(ctx, tgi18n.For(tb.bundle, lang))
		next(ctx, b, update)
	}
}

func (tb *Bot) logMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message != nil && update.Message.From != nil {
			tb.log.Debug("update",
				"update_id", update.ID,
				"telegram_user_id", update.Message.From.ID,
				"language_code", update.Message.From.LanguageCode,
			)
		}
		next(ctx, b, update)
	}
}
