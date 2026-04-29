package handlers

import (
	"context"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
)

const CallbackPrefixWelcome = "welcome:"

func SendWelcome(ctx context.Context, b *bot.Bot, chatID int64) {
	loc := tgi18n.FromContext(ctx)
	text := tgi18n.T(loc, "welcome.text", nil)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: welcomeKeyboard(loc),
	})
}

func welcomeKeyboard(loc *goi18n.Localizer) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: tgi18n.T(loc, "welcome.btn.mywords", nil), CallbackData: CallbackPrefixWelcome + "mywords"},
				{Text: tgi18n.T(loc, "welcome.btn.study", nil), CallbackData: CallbackPrefixWelcome + "study"},
			},
			{
				{Text: tgi18n.T(loc, "welcome.btn.history", nil), CallbackData: CallbackPrefixWelcome + "history"},
				{Text: tgi18n.T(loc, "welcome.btn.settings", nil), CallbackData: CallbackPrefixWelcome + "settings"},
			},
		},
	}
}

func HandleWelcomeCallback(
	myWordsH *MyWords,
	studyH *Study,
	historyH *History,
	settingsH *Settings,
) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		cq := update.CallbackQuery
		if cq == nil {
			return
		}
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})

		action := strings.TrimPrefix(cq.Data, CallbackPrefixWelcome)
		chatID, _, ok := callbackMessageRef(cq)
		if !ok {
			return
		}

		fakeUpdate := &models.Update{
			Message: &models.Message{
				Chat: models.Chat{ID: chatID},
				From: &cq.From,
			},
		}

		switch action {
		case "mywords":
			myWordsH.HandleCommand(ctx, b, fakeUpdate)
		case "study":
			studyH.HandleCommand(ctx, b, fakeUpdate)
		case "history":
			historyH.HandleCommand(ctx, b, fakeUpdate)
		case "settings":
			settingsH.HandleCommand(ctx, b, fakeUpdate)
		}
	}
}
