package handlers

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot/models"
	"github.com/nikita/tg-linguine/internal/users"
)

// resolveCallbackUser registers (or fetches) the Telegram user behind a
// callback query. On any error it logs with the given prefix and returns
// (nil, false) so the caller can early-return without fanning out boilerplate.
// Localization is intentionally left to the caller — the helper does not need
// the i18n bundle.
func resolveCallbackUser(
	ctx context.Context,
	svc *users.Service,
	cq *models.CallbackQuery,
	log *slog.Logger,
	logPrefix string,
) (*users.User, bool) {
	if cq == nil {
		return nil, false
	}
	from := cq.From
	u, _, err := svc.RegisterUser(ctx, users.TelegramUser{
		ID:           from.ID,
		Username:     from.Username,
		FirstName:    from.FirstName,
		LanguageCode: from.LanguageCode,
	})
	if err != nil {
		log.Error(logPrefix+": register", "err", err)
		return nil, false
	}
	return u, true
}

// resolveMessageUser is the same as resolveCallbackUser but for plain
// messages.
func resolveMessageUser(
	ctx context.Context,
	svc *users.Service,
	msg *models.Message,
	log *slog.Logger,
	logPrefix string,
) (*users.User, bool) {
	if msg == nil || msg.From == nil {
		return nil, false
	}
	from := msg.From
	u, _, err := svc.RegisterUser(ctx, users.TelegramUser{
		ID:           from.ID,
		Username:     from.Username,
		FirstName:    from.FirstName,
		LanguageCode: from.LanguageCode,
	})
	if err != nil {
		log.Error(logPrefix+": register", "err", err)
		return nil, false
	}
	return u, true
}
