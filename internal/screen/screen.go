package screen

import "github.com/go-telegram/bot/models"

// ScreenID identifies a UI screen in the bot.
type ScreenID string

const (
	ScreenWelcome     ScreenID = "welcome"
	ScreenOnboarding  ScreenID = "onboarding"
	ScreenMyWords     ScreenID = "mywords"
	ScreenWordEdit    ScreenID = "word_edit"
	ScreenHistory     ScreenID = "history"
	ScreenSettings    ScreenID = "settings"
	ScreenArticleCard ScreenID = "article"
	ScreenSetKey      ScreenID = "setkey"
	ScreenDeleteMe    ScreenID = "delete_me"
	ScreenMe          ScreenID = "me"
	ScreenAnalyzing   ScreenID = "analyzing"
	ScreenLongArticle ScreenID = "long_article"
	ScreenQuizSummary ScreenID = "quiz_summary"
)

// Screen describes a fully-resolved UI screen ready to be rendered.
type Screen struct {
	ID        ScreenID
	Text      string
	Keyboard  *models.InlineKeyboardMarkup
	Parent    ScreenID
	Context   map[string]string
	ParseMode models.ParseMode
}
