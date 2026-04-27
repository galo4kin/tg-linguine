package handlers

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
)

func Start(ctx context.Context, b *bot.Bot, update *models.Update) {
	loc := tgi18n.FromContext(ctx)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   tgi18n.T(loc, "start.greeting", nil),
	})
}
