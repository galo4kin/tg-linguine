package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/session"
	"github.com/nikita/tg-linguine/internal/users"
)

// CallbackPrefixSettings drives the settings menu. Payloads:
//
//	set:menu                 — return to the top menu
//	set:iface                — show interface-language sub-menu
//	set:iface:<code>         — pick interface language (ru/en/es)
//	set:lang                 — show learning-language sub-menu
//	set:lang:<code>          — pick learning language; if a row exists for the
//	                           user we just flip is_active, otherwise we route
//	                           to the CEFR-for-new-language sub-menu.
//	set:cefr                 — show CEFR sub-menu (changes active language)
//	set:cefr:<LEVEL>         — apply CEFR to the user's active language
//	set:cefr_for:<lang>:<L>  — apply CEFR to a freshly added language
//	set:apikey               — kick off the /setkey flow inline
//	set:delete               — open the /delete_me confirmation inline
//	set:noop                 — non-interactive button (used for the "active" badge)
const CallbackPrefixSettings = "set:"

// Settings serves /settings and the `set:` callback family. It owns the
// inline menu and routes each leaf action to the relevant repository or to
// the existing API-key waiter for the `/setkey` reuse case.
type Settings struct {
	users     *users.Service
	languages users.UserLanguageRepository
	keyWaiter *session.APIKeyWaiter
	deleteH   *Delete
	bundle    *goi18n.Bundle
	log       *slog.Logger
}

func NewSettings(
	svc *users.Service,
	langs users.UserLanguageRepository,
	keyWaiter *session.APIKeyWaiter,
	deleteH *Delete,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *Settings {
	return &Settings{
		users:     svc,
		languages: langs,
		keyWaiter: keyWaiter,
		deleteH:   deleteH,
		bundle:    bundle,
		log:       log,
	}
}

// HandleCommand reacts to `/settings` — opens a fresh top menu.
func (h *Settings) HandleCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}
	u, ok := resolveMessageUser(ctx, h.users, msg, h.log, "settings cmd")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      msg.Chat.ID,
		Text:        tgi18n.T(loc, "settings.title", nil),
		ReplyMarkup: settingsTopKeyboard(loc),
	})
}

// HandleCallback drives the `set:` prefix.
func (h *Settings) HandleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	defer func() {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})
	}()

	u, ok := resolveCallbackUser(ctx, h.users, cq, h.log, "settings cb")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	chatID, msgID, ok := callbackMessageRef(cq)
	if !ok {
		return
	}

	payload := strings.TrimPrefix(cq.Data, CallbackPrefixSettings)
	switch {
	case payload == "noop":
		return
	case payload == "menu":
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "settings.title", nil), settingsTopKeyboard(loc))
	case payload == "iface":
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "settings.iface.choose", nil), settingsIfaceKeyboard(loc, u.InterfaceLanguage))
	case strings.HasPrefix(payload, "iface:"):
		h.applyInterface(ctx, b, chatID, msgID, u, strings.TrimPrefix(payload, "iface:"))
	case payload == "lang":
		h.openLanguageMenu(ctx, b, chatID, msgID, u)
	case strings.HasPrefix(payload, "lang:"):
		h.applyLearningLanguage(ctx, b, chatID, msgID, u, strings.TrimPrefix(payload, "lang:"))
	case payload == "cefr":
		h.openCEFRMenu(ctx, b, chatID, msgID, u)
	case strings.HasPrefix(payload, "cefr_for:"):
		// Two more colons: <lang>:<LEVEL>
		rest := strings.TrimPrefix(payload, "cefr_for:")
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 || !users.IsSupportedLearningLanguage(parts[0]) || !users.IsCEFR(parts[1]) {
			h.log.Warn("settings cb: bad cefr_for", "data", cq.Data)
			return
		}
		h.applyNewLanguageCEFR(ctx, b, chatID, msgID, u, parts[0], parts[1])
	case strings.HasPrefix(payload, "cefr:"):
		h.applyActiveCEFR(ctx, b, chatID, msgID, u, strings.TrimPrefix(payload, "cefr:"))
	case payload == "apikey":
		h.startAPIKeyFlow(ctx, b, chatID, msgID, u)
	case payload == "delete":
		if h.deleteH == nil {
			h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "error.generic", nil), settingsBackKeyboard(loc))
			return
		}
		h.deleteH.PromptInline(ctx, b, chatID, msgID, u.InterfaceLanguage)
	default:
		h.log.Warn("settings cb: unknown payload", "data", cq.Data)
	}
}

func (h *Settings) editTo(ctx context.Context, b *bot.Bot, chatID any, msgID int, text string, kb *models.InlineKeyboardMarkup) {
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        text,
		ReplyMarkup: kb,
	}); err != nil {
		h.log.Debug("settings: edit", "err", err)
	}
}

func (h *Settings) applyInterface(ctx context.Context, b *bot.Bot, chatID any, msgID int, u *users.User, lang string) {
	if !users.IsSupportedInterfaceLanguage(lang) {
		return
	}
	if err := h.users.SetInterfaceLanguage(ctx, u.ID, lang); err != nil {
		h.log.Error("settings iface: persist", "err", err, "user_id", u.ID)
		loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "error.generic", nil), settingsBackKeyboard(loc))
		return
	}
	// Localize the confirmation in the NEW language so the change is visible
	// immediately, as the task DoD requires.
	loc := tgi18n.For(h.bundle, lang)
	h.editTo(ctx, b, chatID, msgID,
		tgi18n.T(loc, "settings.iface.applied", map[string]string{"Language": strings.ToUpper(lang)}),
		settingsBackKeyboard(loc),
	)
}

func (h *Settings) openLanguageMenu(ctx context.Context, b *bot.Bot, chatID any, msgID int, u *users.User) {
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	existing, err := h.languages.List(ctx, u.ID)
	if err != nil {
		h.log.Error("settings lang: list", "err", err, "user_id", u.ID)
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "error.generic", nil), settingsBackKeyboard(loc))
		return
	}
	h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "settings.lang.choose", nil), settingsLanguageKeyboard(loc, existing))
}

func (h *Settings) applyLearningLanguage(ctx context.Context, b *bot.Bot, chatID any, msgID int, u *users.User, lang string) {
	if !users.IsSupportedLearningLanguage(lang) {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	err := h.languages.Activate(ctx, u.ID, lang)
	if errors.Is(err, users.ErrNotFound) {
		// New language for this user — ask for CEFR before persisting.
		h.editTo(ctx, b, chatID, msgID,
			tgi18n.T(loc, "settings.cefr.choose_for_new", map[string]string{"Language": strings.ToUpper(lang)}),
			settingsCEFRForNewKeyboard(loc, lang),
		)
		return
	}
	if err != nil {
		h.log.Error("settings lang: activate", "err", err, "user_id", u.ID)
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "error.generic", nil), settingsBackKeyboard(loc))
		return
	}
	h.editTo(ctx, b, chatID, msgID,
		tgi18n.T(loc, "settings.lang.applied", map[string]string{"Language": strings.ToUpper(lang)}),
		settingsBackKeyboard(loc),
	)
}

func (h *Settings) openCEFRMenu(ctx context.Context, b *bot.Bot, chatID any, msgID int, u *users.User) {
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	active, err := h.languages.Active(ctx, u.ID)
	if errors.Is(err, users.ErrNotFound) {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "settings.cefr.no_active", nil), settingsBackKeyboard(loc))
		return
	}
	if err != nil {
		h.log.Error("settings cefr: active", "err", err, "user_id", u.ID)
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "error.generic", nil), settingsBackKeyboard(loc))
		return
	}
	h.editTo(ctx, b, chatID, msgID,
		tgi18n.T(loc, "settings.cefr.choose", map[string]string{"Language": strings.ToUpper(active.LanguageCode)}),
		settingsCEFRKeyboard(loc, active.CEFRLevel),
	)
}

func (h *Settings) applyActiveCEFR(ctx context.Context, b *bot.Bot, chatID any, msgID int, u *users.User, level string) {
	if !users.IsCEFR(level) {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	if err := h.languages.SetCEFR(ctx, u.ID, level); err != nil {
		if errors.Is(err, users.ErrNotFound) {
			h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "settings.cefr.no_active", nil), settingsBackKeyboard(loc))
			return
		}
		h.log.Error("settings cefr: persist", "err", err, "user_id", u.ID)
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "error.generic", nil), settingsBackKeyboard(loc))
		return
	}
	h.editTo(ctx, b, chatID, msgID,
		tgi18n.T(loc, "settings.cefr.applied", map[string]string{"Level": level}),
		settingsBackKeyboard(loc),
	)
}

func (h *Settings) applyNewLanguageCEFR(ctx context.Context, b *bot.Bot, chatID any, msgID int, u *users.User, lang, level string) {
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	if err := h.languages.Set(ctx, u.ID, lang, level); err != nil {
		h.log.Error("settings lang+cefr: persist", "err", err, "user_id", u.ID)
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "error.generic", nil), settingsBackKeyboard(loc))
		return
	}
	h.editTo(ctx, b, chatID, msgID,
		tgi18n.T(loc, "settings.lang.added", map[string]string{
			"Language": strings.ToUpper(lang),
			"Level":    level,
		}),
		settingsBackKeyboard(loc),
	)
}

func (h *Settings) startAPIKeyFlow(ctx context.Context, b *bot.Bot, chatID any, msgID int, u *users.User) {
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	if h.keyWaiter != nil {
		h.keyWaiter.Arm(u.TelegramUserID)
	}
	h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "apikey.prompt", nil), settingsBackKeyboard(loc))
}

func settingsTopKeyboard(loc *goi18n.Localizer) *models.InlineKeyboardMarkup {
	rows := [][]models.InlineKeyboardButton{
		{{Text: tgi18n.T(loc, "settings.btn.iface", nil), CallbackData: CallbackPrefixSettings + "iface"}},
		{{Text: tgi18n.T(loc, "settings.btn.lang", nil), CallbackData: CallbackPrefixSettings + "lang"}},
		{{Text: tgi18n.T(loc, "settings.btn.cefr", nil), CallbackData: CallbackPrefixSettings + "cefr"}},
		{{Text: tgi18n.T(loc, "settings.btn.apikey", nil), CallbackData: CallbackPrefixSettings + "apikey"}},
		{{Text: tgi18n.T(loc, "settings.btn.delete", nil), CallbackData: CallbackPrefixSettings + "delete"}},
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func settingsIfaceKeyboard(loc *goi18n.Localizer, current string) *models.InlineKeyboardMarkup {
	row := make([]models.InlineKeyboardButton, 0, len(users.SupportedInterfaceLanguages))
	for _, code := range users.SupportedInterfaceLanguages {
		label := strings.ToUpper(code)
		if code == current {
			label = "✓ " + label
		}
		row = append(row, models.InlineKeyboardButton{
			Text:         label,
			CallbackData: CallbackPrefixSettings + "iface:" + code,
		})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		row,
		{backToMenuButton(loc)},
	}}
}

func settingsLanguageKeyboard(loc *goi18n.Localizer, existing []users.UserLanguage) *models.InlineKeyboardMarkup {
	activeByCode := map[string]bool{}
	for _, ul := range existing {
		if ul.IsActive {
			activeByCode[ul.LanguageCode] = true
		}
	}
	row := make([]models.InlineKeyboardButton, 0, len(users.SupportedLearningLanguages))
	for _, code := range users.SupportedLearningLanguages {
		label := strings.ToUpper(code)
		if activeByCode[code] {
			label = "✓ " + label
		}
		row = append(row, models.InlineKeyboardButton{
			Text:         label,
			CallbackData: CallbackPrefixSettings + "lang:" + code,
		})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		row,
		{backToMenuButton(loc)},
	}}
}

func settingsCEFRKeyboard(loc *goi18n.Localizer, current string) *models.InlineKeyboardMarkup {
	row := make([]models.InlineKeyboardButton, 0, len(users.CEFRLevels))
	for _, lvl := range users.CEFRLevels {
		label := lvl
		if lvl == current {
			label = "✓ " + label
		}
		row = append(row, models.InlineKeyboardButton{
			Text:         label,
			CallbackData: CallbackPrefixSettings + "cefr:" + lvl,
		})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		row,
		{backToMenuButton(loc)},
	}}
}

func settingsCEFRForNewKeyboard(loc *goi18n.Localizer, lang string) *models.InlineKeyboardMarkup {
	row := make([]models.InlineKeyboardButton, 0, len(users.CEFRLevels))
	for _, lvl := range users.CEFRLevels {
		row = append(row, models.InlineKeyboardButton{
			Text:         lvl,
			CallbackData: fmt.Sprintf("%scefr_for:%s:%s", CallbackPrefixSettings, lang, lvl),
		})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		row,
		{backToMenuButton(loc)},
	}}
}

func settingsBackKeyboard(loc *goi18n.Localizer) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{backToMenuButton(loc)},
	}}
}

func backToMenuButton(loc *goi18n.Localizer) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{
		Text:         tgi18n.T(loc, "settings.btn.back", nil),
		CallbackData: CallbackPrefixSettings + "menu",
	}
}

