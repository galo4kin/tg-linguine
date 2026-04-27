package telegram

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/nikita/tg-linguine/internal/config"
	"github.com/nikita/tg-linguine/internal/telegram/handlers"
)

type Bot struct {
	b   *bot.Bot
	log *slog.Logger
}

func New(cfg *config.Config, log *slog.Logger) (*Bot, error) {
	tb := &Bot{log: log}

	opts := []bot.Option{
		bot.WithMiddlewares(tb.logMiddleware),
	}

	b, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		return nil, err
	}

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, handlers.Start)

	tb.b = b
	return tb, nil
}

func (tb *Bot) Start(ctx context.Context) {
	tb.b.Start(ctx)
}

func (tb *Bot) logMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message != nil {
			tb.log.Debug("update",
				"update_id", update.ID,
				"telegram_user_id", update.Message.From.ID,
				"language_code", update.Message.From.LanguageCode,
			)
		}
		next(ctx, b, update)
	}
}
