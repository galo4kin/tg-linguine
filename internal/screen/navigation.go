package screen

import (
	"encoding/json"

	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
)

const CallbackPrefixNav = "nav:"

// WithNavigation appends nav buttons (Back/Home) to a keyboard.
// parent == "" => only Home; parent != "" => [Back, Home]
// Use WithNavigationFor(loc, body, ScreenWelcome, "", nil) to skip nav entirely.
func WithNavigation(loc *goi18n.Localizer, body *models.InlineKeyboardMarkup, parent ScreenID, ctx map[string]string) *models.InlineKeyboardMarkup {
	return WithNavigationFor(loc, body, "", parent, ctx)
}

// WithNavigationFor: if self == ScreenWelcome, no nav buttons at all.
func WithNavigationFor(loc *goi18n.Localizer, body *models.InlineKeyboardMarkup, self ScreenID, parent ScreenID, ctx map[string]string) *models.InlineKeyboardMarkup {
	if body == nil {
		body = &models.InlineKeyboardMarkup{}
	}
	if self == ScreenWelcome {
		return body
	}
	var navRow []models.InlineKeyboardButton
	if parent != "" {
		ctxJSON, _ := json.Marshal(ctx)
		navRow = append(navRow, models.InlineKeyboardButton{
			Text:         tgi18n.T(loc, "nav.back", nil),
			CallbackData: CallbackPrefixNav + "back:" + string(parent) + ":" + string(ctxJSON),
		})
	}
	navRow = append(navRow, models.InlineKeyboardButton{
		Text:         tgi18n.T(loc, "nav.home", nil),
		CallbackData: CallbackPrefixNav + "home",
	})
	body.InlineKeyboard = append(body.InlineKeyboard, navRow)
	return body
}
