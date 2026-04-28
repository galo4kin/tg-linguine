package handlers

import (
	"context"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/session"
	"github.com/nikita/tg-linguine/internal/users"
)

// CallbackPrefixDelete drives the /delete_me confirmation flow. Payloads:
//
//	del:confirm  — user confirmed, wipe everything
//	del:cancel   — user backed out
const CallbackPrefixDelete = "del:"

// Delete owns the GDPR-style "wipe my data" flow. It serves /delete_me,
// the confirm/cancel callbacks, and a settings entry-point used by the
// "🗑 Delete my data" button. On confirm it runs the transactional wipe in
// users.Service and clears any in-memory FSM state for the same Telegram
// user so that a stale session cannot resurrect references to the deleted
// row.
type Delete struct {
	users     *users.Service
	onbFSM    *session.Onboarding
	studyFSM  *session.Quiz
	keyWaiter *session.APIKeyWaiter
	bundle    *goi18n.Bundle
	log       *slog.Logger
}

func NewDelete(
	svc *users.Service,
	onbFSM *session.Onboarding,
	studyFSM *session.Quiz,
	keyWaiter *session.APIKeyWaiter,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *Delete {
	return &Delete{
		users:     svc,
		onbFSM:    onbFSM,
		studyFSM:  studyFSM,
		keyWaiter: keyWaiter,
		bundle:    bundle,
		log:       log,
	}
}

// HandleCommand reacts to /delete_me — sends a fresh confirmation prompt
// with «Yes, delete» / «Cancel» inline buttons.
func (h *Delete) HandleCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}
	u, ok := resolveMessageUser(ctx, h.users, msg, h.log, "delete cmd")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      msg.Chat.ID,
		Text:        tgi18n.T(loc, "delete.confirm.text", nil),
		ReplyMarkup: deleteConfirmKeyboard(loc),
	})
}

// PromptInline edits an existing message into the confirmation prompt —
// used by the settings menu's "Delete my data" button so we don't spawn a
// second message for the same flow.
func (h *Delete) PromptInline(ctx context.Context, b *bot.Bot, chatID any, msgID int, ifaceLang string) {
	loc := tgi18n.For(h.bundle, ifaceLang)
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        tgi18n.T(loc, "delete.confirm.text", nil),
		ReplyMarkup: deleteConfirmKeyboard(loc),
	}); err != nil {
		h.log.Debug("delete: prompt edit", "err", err)
	}
}

// HandleCallback drives the `del:` prefix.
func (h *Delete) HandleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	defer func() {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})
	}()

	u, ok := resolveCallbackUser(ctx, h.users, cq, h.log, "delete cb")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	chatID, msgID, ok := callbackMessageRef(cq)
	if !ok {
		return
	}

	switch strings.TrimPrefix(cq.Data, CallbackPrefixDelete) {
	case "confirm":
		h.confirm(ctx, b, u, loc, chatID, msgID)
	case "cancel":
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "delete.cancelled", nil), nil)
	default:
		h.log.Warn("delete cb: unknown payload", "data", cq.Data)
	}
}

func (h *Delete) confirm(ctx context.Context, b *bot.Bot, u *users.User, loc *goi18n.Localizer, chatID any, msgID int) {
	if err := h.users.DeleteUser(ctx, u.ID); err != nil {
		h.log.Error("delete: persist", "err", err, "user_id", u.ID)
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "error.generic", nil), nil)
		return
	}
	h.resetFSM(u)
	h.log.Info("user deleted", "user_id", u.ID, "telegram_user_id", u.TelegramUserID)
	h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "delete.done", nil), nil)
}

// resetFSM drops every in-memory session bound to the user. The onboarding
// FSM and api-key waiter key by Telegram user ID; the study FSM keys by
// internal user ID. We clear all three so a stale callback cannot resurrect
// the deleted row.
func (h *Delete) resetFSM(u *users.User) {
	if h.onbFSM != nil {
		h.onbFSM.Clear(u.TelegramUserID)
	}
	if h.keyWaiter != nil {
		h.keyWaiter.Disarm(u.TelegramUserID)
	}
	if h.studyFSM != nil {
		h.studyFSM.End(u.ID)
	}
}

func (h *Delete) editTo(ctx context.Context, b *bot.Bot, chatID any, msgID int, text string, kb *models.InlineKeyboardMarkup) {
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        text,
		ReplyMarkup: kb,
	}); err != nil {
		h.log.Debug("delete: edit", "err", err)
	}
}

func deleteConfirmKeyboard(loc *goi18n.Localizer) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{
			{Text: tgi18n.T(loc, "delete.btn.confirm", nil), CallbackData: CallbackPrefixDelete + "confirm"},
			{Text: tgi18n.T(loc, "delete.btn.cancel", nil), CallbackData: CallbackPrefixDelete + "cancel"},
		},
	}}
}
