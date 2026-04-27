package handlers

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/llm"
	"github.com/nikita/tg-linguine/internal/session"
	"github.com/nikita/tg-linguine/internal/users"
)

type APIKey struct {
	users    *users.Service
	keys     users.APIKeyRepository
	provider llm.Provider
	waiter   *session.APIKeyWaiter
	bundle   *goi18n.Bundle
	log      *slog.Logger
}

func NewAPIKey(
	svc *users.Service,
	keys users.APIKeyRepository,
	provider llm.Provider,
	waiter *session.APIKeyWaiter,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *APIKey {
	return &APIKey{users: svc, keys: keys, provider: provider, waiter: waiter, bundle: bundle, log: log}
}

// HandleSetKeyCommand reacts to `/setkey` and arms the FSM for the next text message.
func (h *APIKey) HandleSetKeyCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	from := update.Message.From
	u, _, err := h.users.RegisterUser(ctx, users.TelegramUser{
		ID: from.ID, Username: from.Username, FirstName: from.FirstName, LanguageCode: from.LanguageCode,
	})
	if err != nil {
		h.log.Error("setkey: register", "err", err)
		return
	}
	h.waiter.Arm(u.TelegramUserID)
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   tgi18n.T(loc, "apikey.prompt", nil),
	})
}

// HandleIncomingText is registered as a fallback for plain text messages —
// it consumes the message only if the user is currently in `awaiting_api_key`.
func (h *APIKey) HandleIncomingText(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}
	if !h.waiter.IsArmed(msg.From.ID) {
		return
	}
	from := msg.From
	u, _, err := h.users.RegisterUser(ctx, users.TelegramUser{
		ID: from.ID, Username: from.Username, FirstName: from.FirstName, LanguageCode: from.LanguageCode,
	})
	if err != nil {
		h.log.Error("apikey ingest: register", "err", err)
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	key := strings.TrimSpace(msg.Text)
	if key == "" {
		return
	}
	if !strings.HasPrefix(key, "gsk_") {
		h.log.Info("api key prefix mismatch", "user_id", u.ID)
	}

	if err := h.provider.ValidateAPIKey(ctx, key); err != nil {
		h.log.Warn("api key validation failed",
			"user_id", u.ID,
			"reason", classifyKeyError(err),
		)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   tgi18n.T(loc, errorMessageID(err), nil),
		})
		return
	}

	if err := h.keys.Set(ctx, u.ID, users.ProviderGroq, key); err != nil {
		h.log.Error("apikey persist", "user_id", u.ID, "err", err)
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   tgi18n.T(loc, "error.generic", nil),
		})
		return
	}
	h.waiter.Disarm(msg.From.ID)
	h.log.Info("api key stored", "user_id", u.ID, "provider", users.ProviderGroq)

	// Best-effort: scrub the user's message so the plaintext key disappears from chat history.
	if _, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    msg.Chat.ID,
		MessageID: msg.ID,
	}); err != nil {
		h.log.Debug("delete user key message failed", "err", err)
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text:   tgi18n.T(loc, "apikey.saved", nil),
	})
}

func classifyKeyError(err error) string {
	switch {
	case errors.Is(err, llm.ErrInvalidAPIKey):
		return "invalid"
	case errors.Is(err, llm.ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, llm.ErrUnavailable):
		return "unavailable"
	default:
		return "other"
	}
}

func errorMessageID(err error) string {
	switch {
	case errors.Is(err, llm.ErrInvalidAPIKey):
		return "apikey.invalid"
	case errors.Is(err, llm.ErrRateLimited):
		return "apikey.rate_limited"
	case errors.Is(err, llm.ErrUnavailable):
		return "apikey.unavailable"
	default:
		return "error.generic"
	}
}
