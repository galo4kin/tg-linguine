package telegram

import (
	"context"
	"database/sql"
	"log/slog"
	"runtime/debug"
	"sync"
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
	users  *users.Service
	// inflight tracks handlers currently running so Shutdown can drain them
	// gracefully on SIGTERM. The recover/wait middleware Add()s before
	// dispatch and Done()s in defer.
	inflight sync.WaitGroup
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
	Dictionary   dictionary.Repository
	DB           *sql.DB
}

func New(cfg *config.Config, log *slog.Logger, deps Deps) (*Bot, error) {
	tb := &Bot{log: log, bundle: deps.Bundle, users: deps.Users}

	opts := []bot.Option{
		// Order matters: track-inflight first so Shutdown sees the work; then
		// recover so panics are caught before they unwind through the bot
		// loop; then i18n/log/touch so handlers run with localizer in context
		// and the user's last_seen_at gets bumped before dispatch.
		bot.WithMiddlewares(tb.trackInflightMiddleware, tb.recoverMiddleware, tb.i18nMiddleware, tb.logMiddleware, tb.touchLastSeenMiddleware),
	}

	b, err := bot.New(cfg.BotToken, opts...)
	if err != nil {
		return nil, err
	}

	onbFSM := session.NewOnboarding(onboardingTTL)
	onb := handlers.NewOnboarding(deps.Users, deps.Languages, onbFSM, deps.Bundle, log)

	keyWaiter := session.NewAPIKeyWaiter(apiKeyPromptTTL)
	apiKey := handlers.NewAPIKey(deps.Users, deps.APIKeys, deps.LLMProvider, keyWaiter, deps.Bundle, log)

	urlLimiter := NewURLRateLimiter(cfg.RateLimitPerHour, time.Hour)
	urlH := handlers.NewURL(deps.Users, deps.Languages, deps.Articles, deps.ArticleRepo, deps.ArticleWords, deps.DB, urlLimiter, deps.Bundle, log)
	wordsH := handlers.NewWords(deps.Users, deps.ArticleRepo, deps.ArticleWords, deps.WordStatuses, deps.DB, deps.Bundle, log)
	historyH := handlers.NewHistory(deps.Users, deps.Languages, deps.ArticleRepo, deps.ArticleWords, deps.Articles, deps.DB, deps.Bundle, log)
	cardH := handlers.NewCard(deps.Users, deps.Languages, deps.ArticleRepo, deps.ArticleWords, deps.Articles, deps.DB, deps.Bundle, log)
	myWordsH := handlers.NewMyWords(deps.Users, deps.Languages, deps.WordStatuses, deps.DB, deps.Bundle, log)
	studyFSM := session.NewStudy(studySessionTTL)
	studyH := handlers.NewStudy(deps.Users, deps.Languages, deps.WordStatuses, studyFSM, deps.DB, deps.Bundle, log)
	deleteH := handlers.NewDelete(deps.Users, onbFSM, studyFSM, keyWaiter, deps.Bundle, log)
	settingsH := handlers.NewSettings(deps.Users, deps.Languages, keyWaiter, deleteH, deps.Bundle, log)
	adminGate := func(uid int64) bool { return IsAdmin(cfg, uid) }
	adminH := handlers.NewAdmin(adminGate, deps.Users, deps.ArticleRepo, deps.Dictionary, deps.DB, log)

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact,
		handlers.Start(deps.Users, deps.Languages, onb, deps.Bundle, log))
	b.RegisterHandler(bot.HandlerTypeMessageText, "/setkey", bot.MatchTypeExact, apiKey.HandleSetKeyCommand)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/history", bot.MatchTypeExact, historyH.HandleCommand)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/settings", bot.MatchTypeExact, settingsH.HandleCommand)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/mywords", bot.MatchTypeExact, myWordsH.HandleCommand)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/study", bot.MatchTypeExact, studyH.HandleCommand)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/delete_me", bot.MatchTypeExact, deleteH.HandleCommand)
	// Admin commands. The handlers themselves silently no-op for non-admins
	// (see handlers/admin.go), so registering them globally does not leak the
	// admin surface to regular users.
	b.RegisterHandler(bot.HandlerTypeMessageText, "/stats", bot.MatchTypeExact, adminH.HandleStats)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/shutdown", bot.MatchTypeExact, adminH.HandleShutdown)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/whoami", bot.MatchTypeExact, adminH.HandleWhoami)
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
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handlers.CallbackPrefixDelete, bot.MatchTypePrefix, deleteH.HandleCallback)

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

// Shutdown waits for in-flight handlers to finish, capped at `timeout`.
// Returns true if everything drained cleanly, false if the timeout fired
// (callers may still exit — handlers will continue best-effort, but the
// process is on its way out).
func (tb *Bot) Shutdown(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		tb.inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (tb *Bot) trackInflightMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		tb.inflight.Add(1)
		defer tb.inflight.Done()
		next(ctx, b, update)
	}
}

// recoverMiddleware turns a panic in any handler into a localized "something
// went wrong" reply, so a single buggy code path never tears down the bot.
// The full stack is logged at error level with errors_total=1 for log-based
// alerting.
func (tb *Bot) recoverMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			tb.log.Error("handler panic recovered",
				"errors_total", 1,
				"panic", r,
				"stack", string(debug.Stack()),
			)
			loc := tgi18n.For(tb.bundle, langFromUpdate(update))
			msg := tgi18n.T(loc, "error.generic", nil)
			if chatID := chatIDFromUpdate(update); chatID != 0 {
				_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: msg})
			}
		}()
		next(ctx, b, update)
	}
}

func langFromUpdate(u *models.Update) string {
	switch {
	case u == nil:
		return "en"
	case u.Message != nil && u.Message.From != nil:
		return u.Message.From.LanguageCode
	case u.CallbackQuery != nil:
		return u.CallbackQuery.From.LanguageCode
	}
	return "en"
}

func chatIDFromUpdate(u *models.Update) int64 {
	switch {
	case u == nil:
		return 0
	case u.Message != nil:
		return u.Message.Chat.ID
	case u.CallbackQuery != nil && u.CallbackQuery.Message.Message != nil:
		return u.CallbackQuery.Message.Message.Chat.ID
	}
	return 0
}

func (tb *Bot) i18nMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		ctx = tgi18n.WithLocalizer(ctx, tgi18n.For(tb.bundle, langFromUpdate(update)))
		next(ctx, b, update)
	}
}

// touchLastSeenMiddleware bumps `users.last_seen_at` to NOW for the
// Telegram-id behind every incoming update. Failures are logged but do not
// short-circuit the handler — the activity counter is best-effort, not a
// gate. We deliberately do this AFTER the panic-recovery middleware so a
// panic in the touch path does not break dispatch.
func (tb *Bot) touchLastSeenMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		var tgID int64
		switch {
		case update.Message != nil && update.Message.From != nil:
			tgID = update.Message.From.ID
		case update.CallbackQuery != nil:
			tgID = update.CallbackQuery.From.ID
		}
		if tgID != 0 && tb.users != nil {
			if err := tb.users.TouchLastSeen(ctx, tgID); err != nil {
				tb.log.Warn("touch last_seen failed",
					"telegram_user_id", tgID,
					"err", err,
				)
			}
		}
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
