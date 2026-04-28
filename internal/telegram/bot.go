package telegram

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/config"
	"github.com/nikita/tg-linguine/internal/dictionary"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/llm"
	"github.com/nikita/tg-linguine/internal/session"
	"github.com/nikita/tg-linguine/internal/telegram/handlers"
	"github.com/nikita/tg-linguine/internal/users"
)

const (
	onboardingTTL   = 30 * time.Minute
	apiKeyPromptTTL = 5 * time.Minute
	studySessionTTL = 30 * time.Minute
)

type Bot struct {
	b      *bot.Bot
	log    *slog.Logger
	bundle *goi18n.Bundle
}

type Deps struct {
	Bundle       *goi18n.Bundle
	Users        *users.Service
	Languages    users.UserLanguageRepository
	APIKeys      users.APIKeyRepository
	LLMProvider  llm.Provider
	Articles     *articles.Service
	ArticleRepo  articles.Repository
	ArticleWords dictionary.ArticleWordsRepository
	WordStatuses dictionary.UserWordStatusRepository
	DB           *sql.DB
}

func New(cfg *config.Config, log *slog.Logger, deps Deps) (*Bot, error) {
	tb := &Bot{log: log, bundle: deps.Bundle}

	opts := []bot.Option{
		bot.WithMiddlewares(tb.i18nMiddleware, tb.logMiddleware),
	}

	b, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		return nil, err
	}

	onbFSM := session.NewOnboarding(onboardingTTL)
	onb := handlers.NewOnboarding(deps.Users, deps.Languages, onbFSM, deps.Bundle, log)

	keyWaiter := session.NewAPIKeyWaiter(apiKeyPromptTTL)
	apiKey := handlers.NewAPIKey(deps.Users, deps.APIKeys, deps.LLMProvider, keyWaiter, deps.Bundle, log)

	urlH := handlers.NewURL(deps.Users, deps.Languages, deps.Articles, deps.ArticleRepo, deps.ArticleWords, deps.DB, deps.Bundle, log)
	wordsH := handlers.NewWords(deps.Users, deps.ArticleRepo, deps.ArticleWords, deps.WordStatuses, deps.DB, deps.Bundle, log)
	historyH := handlers.NewHistory(deps.Users, deps.Languages, deps.ArticleRepo, deps.ArticleWords, deps.Articles, deps.DB, deps.Bundle, log)
	cardH := handlers.NewCard(deps.Users, deps.Languages, deps.ArticleRepo, deps.ArticleWords, deps.Articles, deps.DB, deps.Bundle, log)
	settingsH := handlers.NewSettings(deps.Users, deps.Languages, keyWaiter, deps.Bundle, log)
	myWordsH := handlers.NewMyWords(deps.Users, deps.Languages, deps.WordStatuses, deps.DB, deps.Bundle, log)
	studyFSM := session.NewStudy(studySessionTTL)
	studyH := handlers.NewStudy(deps.Users, deps.Languages, deps.WordStatuses, studyFSM, deps.DB, deps.Bundle, log)

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact,
		handlers.Start(deps.Users, deps.Languages, onb, deps.Bundle, log))
	b.RegisterHandler(bot.HandlerTypeMessageText, "/setkey", bot.MatchTypeExact, apiKey.HandleSetKeyCommand)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/history", bot.MatchTypeExact, historyH.HandleCommand)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/settings", bot.MatchTypeExact, settingsH.HandleCommand)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/mywords", bot.MatchTypeExact, myWordsH.HandleCommand)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/study", bot.MatchTypeExact, studyH.HandleCommand)
	b.RegisterHandlerMatchFunc(matchURLText(keyWaiter), urlH.Handle)
	b.RegisterHandlerMatchFunc(matchAPIKeyText(keyWaiter), apiKey.HandleIncomingText)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handlers.CallbackPrefixOnbLang, bot.MatchTypePrefix, onb.HandleLanguage)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handlers.CallbackPrefixOnbLevel, bot.MatchTypePrefix, onb.HandleLevel)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handlers.CallbackPrefixWords, bot.MatchTypePrefix, wordsH.HandleCallback)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handlers.CallbackPrefixWordStatus, bot.MatchTypePrefix, wordsH.HandleStatusCallback)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handlers.CallbackPrefixHistory, bot.MatchTypePrefix, historyH.HandleCallback)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handlers.CallbackPrefixCard, bot.MatchTypePrefix, cardH.HandleCallback)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handlers.CallbackPrefixSettings, bot.MatchTypePrefix, settingsH.HandleCallback)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handlers.CallbackPrefixMyWords, bot.MatchTypePrefix, myWordsH.HandleCallback)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handlers.CallbackPrefixStudy, bot.MatchTypePrefix, studyH.HandleCallback)

	tb.b = b
	return tb, nil
}

// matchURLText fires for any text message that contains an http(s) URL,
// unless the user is currently entering an API key (then the API-key handler
// gets first dibs on the message).
func matchURLText(w *session.APIKeyWaiter) func(*models.Update) bool {
	return func(u *models.Update) bool {
		if u.Message == nil || u.Message.From == nil {
			return false
		}
		if w.IsArmed(u.Message.From.ID) {
			return false
		}
		return handlers.MatchURLMessage(u)
	}
}

func matchAPIKeyText(w *session.APIKeyWaiter) func(*models.Update) bool {
	return func(u *models.Update) bool {
		if u.Message == nil || u.Message.From == nil || u.Message.Text == "" {
			return false
		}
		// Don't intercept other commands.
		if len(u.Message.Text) > 0 && u.Message.Text[0] == '/' {
			return false
		}
		return w.IsArmed(u.Message.From.ID)
	}
}

func (tb *Bot) Start(ctx context.Context) {
	tb.b.Start(ctx)
}

func (tb *Bot) i18nMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		lang := "en"
		if update.Message != nil && update.Message.From != nil {
			lang = update.Message.From.LanguageCode
		} else if update.CallbackQuery != nil {
			lang = update.CallbackQuery.From.LanguageCode
		}
		ctx = tgi18n.WithLocalizer(ctx, tgi18n.For(tb.bundle, lang))
		next(ctx, b, update)
	}
}

func (tb *Bot) logMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		switch {
		case update.Message != nil && update.Message.From != nil:
			tb.log.Debug("update",
				"update_id", update.ID,
				"telegram_user_id", update.Message.From.ID,
				"language_code", update.Message.From.LanguageCode,
			)
		case update.CallbackQuery != nil:
			tb.log.Debug("update",
				"update_id", update.ID,
				"telegram_user_id", update.CallbackQuery.From.ID,
				"callback_data", update.CallbackQuery.Data,
			)
		}
		next(ctx, b, update)
	}
}
