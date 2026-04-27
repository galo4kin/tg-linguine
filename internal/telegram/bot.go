package telegram

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/config"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/session"
	"github.com/nikita/tg-linguine/internal/telegram/handlers"
	"github.com/nikita/tg-linguine/internal/users"
)

const onboardingTTL = 30 * time.Minute

type Bot struct {
	b      *bot.Bot
	log    *slog.Logger
	bundle *goi18n.Bundle
}

func New(
	cfg *config.Config,
	log *slog.Logger,
	bundle *goi18n.Bundle,
	usersSvc *users.Service,
	langs users.UserLanguageRepository,
) (*Bot, error) {
	tb := &Bot{log: log, bundle: bundle}

	opts := []bot.Option{
		bot.WithMiddlewares(tb.i18nMiddleware, tb.logMiddleware),
	}

	b, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		return nil, err
	}

	onbFSM := session.NewOnboarding(onboardingTTL)
	onb := handlers.NewOnboarding(usersSvc, langs, onbFSM, bundle, log)

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact,
		handlers.Start(usersSvc, langs, onb, bundle, log))
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handlers.CallbackPrefixOnbLang, bot.MatchTypePrefix, onb.HandleLanguage)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handlers.CallbackPrefixOnbLevel, bot.MatchTypePrefix, onb.HandleLevel)

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
		} else if update.CallbackQuery != nil {
			lang = update.CallbackQuery.From.LanguageCode
		}
		ctx = tgi18n.WithLocalizer(ctx, tgi18n.For(tb.bundle, lang))
		next(ctx, b, update)
	}
}

func (tb *Bot) logMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		switch {
		case update.Message != nil && update.Message.From != nil:
			tb.log.Debug("update",
				"update_id", update.ID,
				"telegram_user_id", update.Message.From.ID,
				"language_code", update.Message.From.LanguageCode,
			)
		case update.CallbackQuery != nil:
			tb.log.Debug("update",
				"update_id", update.ID,
				"telegram_user_id", update.CallbackQuery.From.ID,
				"callback_data", update.CallbackQuery.Data,
			)
		}
		next(ctx, b, update)
	}
}
