package handlers

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/users"
)

func Start(svc *users.Service, bundle *goi18n.Bundle, log *slog.Logger) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		if update.Message == nil || update.Message.From == nil {
			return
		}
		from := update.Message.From
		u, created, err := svc.RegisterUser(ctx, users.TelegramUser{
			ID:           from.ID,
			Username:     from.Username,
			FirstName:    from.FirstName,
			LanguageCode: from.LanguageCode,
		})
		if err != nil {
			log.Error("register user", "telegram_user_id", from.ID, "err", err)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   tgi18n.T(tgi18n.FromContext(ctx), "error.generic", nil),
			})
			return
		}
		log.Info("/start", "user_id", u.ID, "telegram_user_id", u.TelegramUserID, "created", created)

		loc := tgi18n.For(bundle, u.InterfaceLanguage)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   tgi18n.T(loc, "start.greeting", nil),
		})
	}
}
