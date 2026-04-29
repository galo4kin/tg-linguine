package middleware

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type ctxKey int

const skipDeleteKey ctxKey = 1

// Cleanup returns a middleware that deletes user messages after the handler runs.
// Only acts on message updates in private chats.
func Cleanup(log *slog.Logger) func(bot.HandlerFunc) bot.HandlerFunc {
	return func(next bot.HandlerFunc) bot.HandlerFunc {
		return func(ctx context.Context, b *bot.Bot, update *models.Update) {
			skip := false
			ctx = context.WithValue(ctx, skipDeleteKey, &skip)
			next(ctx, b, update)
			if skip {
				return
			}
			if update.Message == nil {
				return
			}
			if update.Message.Chat.Type != models.ChatTypePrivate {
				return
			}
			_, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{
				ChatID:    update.Message.Chat.ID,
				MessageID: update.Message.ID,
			})
			if err != nil {
				log.Debug("cleanup: delete user message failed",
					"chat", update.Message.Chat.ID, "msg", update.Message.ID, "err", err)
			}
		}
	}
}

// SkipDeleteFromContext tells the cleanup middleware to leave this message.
// Handlers call this to opt out (e.g. URL handler: article links stay).
func SkipDeleteFromContext(ctx context.Context) {
	if p, ok := ctx.Value(skipDeleteKey).(*bool); ok && p != nil {
		*p = true
	}
}
