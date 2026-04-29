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

const (
	CallbackPrefixOnbLang  = "onb:lang:"
	CallbackPrefixOnbLevel = "onb:level:"
)

type Onboarding struct {
	users     *users.Service
	languages users.UserLanguageRepository
	fsm       *session.Onboarding
	welcomeH  *Welcome
	bundle    *goi18n.Bundle
	log       *slog.Logger
}

func NewOnboarding(svc *users.Service, langs users.UserLanguageRepository, fsm *session.Onboarding, welcomeH *Welcome, bundle *goi18n.Bundle, log *slog.Logger) *Onboarding {
	return &Onboarding{users: svc, languages: langs, fsm: fsm, welcomeH: welcomeH, bundle: bundle, log: log}
}

// Begin sends the language-selection keyboard for a fresh onboarding session.
func (h *Onboarding) Begin(ctx context.Context, b *bot.Bot, chatID int64, userID int64, ifaceLang string) {
	h.fsm.Start(userID)
	loc := tgi18n.For(h.bundle, ifaceLang)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        tgi18n.T(loc, "onb.choose_language", nil),
		ReplyMarkup: languageKeyboard(),
	})
}

// Resume re-sends the keyboard for whichever step the user is currently on.
func (h *Onboarding) Resume(ctx context.Context, b *bot.Bot, chatID int64, userID int64, ifaceLang string) {
	snap, ok := h.fsm.Snapshot(userID)
	if !ok {
		h.Begin(ctx, b, chatID, userID, ifaceLang)
		return
	}
	loc := tgi18n.For(h.bundle, ifaceLang)
	switch snap.State {
	case session.StateAwaitingLevel:
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        tgi18n.T(loc, "onb.choose_level", nil),
			ReplyMarkup: levelKeyboard(),
		})
	default:
		h.Begin(ctx, b, chatID, userID, ifaceLang)
	}
}

// HandleLanguage is invoked on `onb:lang:<code>` callbacks.
func (h *Onboarding) HandleLanguage(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	lang := strings.TrimPrefix(cq.Data, CallbackPrefixOnbLang)
	if !users.IsSupportedInterfaceLanguage(lang) {
		h.answer(ctx, b, cq.ID)
		return
	}
	u, ok := resolveCallbackUser(ctx, h.users, cq, h.log, "onb lang")
	if !ok {
		h.answer(ctx, b, cq.ID)
		return
	}
	h.fsm.SetLanguage(u.TelegramUserID, lang)
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)

	chatID, msgID, ok := callbackMessageRef(cq)
	if ok {
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   msgID,
			Text:        tgi18n.T(loc, "onb.choose_level", nil),
			ReplyMarkup: levelKeyboard(),
		})
	}
	h.answer(ctx, b, cq.ID)
}

// HandleLevel is invoked on `onb:level:<code>` callbacks.
func (h *Onboarding) HandleLevel(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	level := strings.TrimPrefix(cq.Data, CallbackPrefixOnbLevel)
	if !users.IsCEFR(level) {
		h.answer(ctx, b, cq.ID)
		return
	}
	u, ok := resolveCallbackUser(ctx, h.users, cq, h.log, "onb level")
	if !ok {
		h.answer(ctx, b, cq.ID)
		return
	}
	snap, ok := h.fsm.Snapshot(u.TelegramUserID)
	if !ok || snap.Language == "" {
		h.answer(ctx, b, cq.ID)
		return
	}

	if err := h.languages.Set(ctx, u.ID, snap.Language, level); err != nil {
		h.log.Error("onb level: persist", "err", err)
		h.answer(ctx, b, cq.ID)
		return
	}
	h.fsm.SetLevel(u.TelegramUserID, level)
	h.fsm.Clear(u.TelegramUserID)

	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	doneText := tgi18n.T(loc, "onb.done", map[string]string{
		"Language": strings.ToUpper(snap.Language),
		"Level":    level,
	})
	chatID, msgID, ok := callbackMessageRef(cq)
	if ok {
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: msgID,
			Text:      doneText,
		})
	}
	h.log.Info("onboarding complete", "user_id", u.ID, "language", snap.Language, "level", level)
	h.welcomeH.Show(ctx, b, chatID)
	h.answer(ctx, b, cq.ID)
}

func (h *Onboarding) answer(ctx context.Context, b *bot.Bot, callbackQueryID string) {
	b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: callbackQueryID})
}

func callbackMessageRef(cq *models.CallbackQuery) (int64, int, bool) {
	if cq.Message.Message == nil {
		return 0, 0, false
	}
	return cq.Message.Message.Chat.ID, cq.Message.Message.ID, true
}

func languageKeyboard() *models.InlineKeyboardMarkup {
	row := make([]models.InlineKeyboardButton, 0, len(users.SupportedInterfaceLanguages))
	for _, code := range users.SupportedInterfaceLanguages {
		row = append(row, models.InlineKeyboardButton{
			Text:         strings.ToUpper(code),
			CallbackData: CallbackPrefixOnbLang + code,
		})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{row}}
}

func levelKeyboard() *models.InlineKeyboardMarkup {
	row := make([]models.InlineKeyboardButton, 0, len(users.CEFRLevels))
	for _, code := range users.CEFRLevels {
		row = append(row, models.InlineKeyboardButton{
			Text:         code,
			CallbackData: CallbackPrefixOnbLevel + code,
		})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{row}}
}
