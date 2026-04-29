package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/dictionary"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/llm"
	"github.com/nikita/tg-linguine/internal/users"
)

// urlRe matches the first http(s) URL in a message body. Whitespace and
// common closing punctuation terminate the URL.
var urlRe = regexp.MustCompile(`https?://[^\s<>"']+`)

// URLHandler runs the AnalyzeArticle pipeline for any message whose text
// contains an http(s) URL.
type URLHandler struct {
	users     *users.Service
	languages users.UserLanguageRepository
	articles  *articles.Service
	render    *cardRenderer
	limiter   URLRateLimiter
	bundle    *goi18n.Bundle
	log       *slog.Logger
}

// CallbackPrefixLongArticle drives the user's choice between the two
// long-article fallback strategies. Payload shape: `lng:<id>:<variant>`
// with variant ∈ {t, s} for truncate / summarize. The id is a short
// random key allocated by articles.PendingStore.
const CallbackPrefixLongArticle = "lng:"

// URLRateLimiter is the small surface the URL handler needs from the
// transport-level limiter. Defining the interface here keeps the handler
// package free of any cross-package dependency on the limiter
// implementation while still letting tests inject a stub.
type URLRateLimiter interface {
	Allow(userID int64) (ok bool, retryAfter time.Duration)
	Capacity() int
}

func NewURL(
	svc *users.Service,
	languages users.UserLanguageRepository,
	articleSvc *articles.Service,
	articleRepo articles.Repository,
	awords dictionary.ArticleWordsRepository,
	db *sql.DB,
	limiter URLRateLimiter,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *URLHandler {
	return &URLHandler{
		users:     svc,
		languages: languages,
		articles:  articleSvc,
		render:    newCardRenderer(log, articleRepo, awords, articleSvc, db),
		limiter:   limiter,
		bundle:    bundle,
		log:       log,
	}
}

func MatchURLMessage(u *models.Update) bool {
	if u.Message == nil || u.Message.From == nil || u.Message.Text == "" {
		return false
	}
	if strings.HasPrefix(u.Message.Text, "/") {
		return false
	}
	return urlRe.MatchString(u.Message.Text)
}

func (h *URLHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}

	u, ok := resolveMessageUser(ctx, h.users, msg, h.log, "url")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)

	url := urlRe.FindString(msg.Text)
	if url == "" {
		return
	}

	if h.limiter != nil {
		if ok, retryAfter := h.limiter.Allow(u.ID); !ok {
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: msg.Chat.ID,
				Text: tgi18n.T(loc, "article.err.rate_limited", map[string]int{
					"Limit":   h.limiter.Capacity(),
					"Minutes": minutesUntil(retryAfter),
				}),
			})
			return
		}
	}

	statusMsg, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text:   tgi18n.T(loc, "article.fetching", nil),
	})
	if err != nil {
		h.log.Error("url: send status", "err", err)
		return
	}

	editStatus := func(text string) {
		if statusMsg == nil {
			return
		}
		if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    msg.Chat.ID,
			MessageID: statusMsg.ID,
			Text:      text,
		}); err != nil {
			h.log.Debug("url: edit status", "err", err)
		}
	}

	progress := func(s articles.Stage) {
		switch s {
		case articles.StageFetching:
			editStatus(tgi18n.T(loc, "article.fetching", nil))
		case articles.StageAnalyzing:
			editStatus(tgi18n.T(loc, "article.analyzing", nil))
		case articles.StageExtractingVocab:
			editStatus(tgi18n.T(loc, "article.extracting_vocab", nil))
		case articles.StagePersisting:
			editStatus(tgi18n.T(loc, "article.persisting", nil))
		}
	}

	result, err := h.articles.AnalyzeArticle(ctx, u.ID, url, progress)
	if err != nil {
		h.log.Warn("url: analyze failed", "user_id", u.ID, "err", err)
		editStatus(tgi18n.T(loc, articleErrorMessageID(err), nil))
		return
	}

	if statusMsg == nil {
		return
	}

	if result.LongPending != nil {
		h.editLongArticlePrompt(ctx, b, msg.Chat.ID, statusMsg.ID, loc, result.LongPending)
		return
	}

	h.renderResult(ctx, b, msg.Chat.ID, statusMsg.ID, loc, u.ID, result.Article)
}

// editLongArticlePrompt overwrites the status message with a localized
// "this article is long, what should I do?" question and two inline-keyboard
// buttons (truncate / summarize).
func (h *URLHandler) editLongArticlePrompt(ctx context.Context, b *bot.Bot, chatID int64, msgID int, loc *goi18n.Localizer, lp *articles.LongPending) {
	text := tgi18n.T(loc, "article.long.prompt", map[string]int{"Words": lp.Words})
	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{
					Text:         tgi18n.T(loc, "article.long.btn.truncate", nil),
					CallbackData: CallbackPrefixLongArticle + lp.PendingID + ":t",
				},
			},
			{
				{
					Text:         tgi18n.T(loc, "article.long.btn.summarize", nil),
					CallbackData: CallbackPrefixLongArticle + lp.PendingID + ":s",
				},
			},
		},
	}
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        text,
		ReplyMarkup: kb,
	}); err != nil {
		h.log.Debug("url: long prompt edit", "err", err)
	}
}

// renderResult is the shared "render an analyzed article into the message"
// step used both by the short-article path (returned directly from
// AnalyzeArticle) and the long-article callback path (returned from
// AnalyzeExtracted). It looks up the user's CEFR and dispatches to the
// shared cardRenderer.
func (h *URLHandler) renderResult(ctx context.Context, b *bot.Bot, chatID int64, msgID int, loc *goi18n.Localizer, userID int64, analyzed *articles.AnalyzedArticle) {
	preview := make([]string, 0, len(analyzed.Words))
	for _, w := range analyzed.Words {
		preview = append(preview, w.Lemma)
	}
	view := DefaultCardView()

	userCEFR := ""
	if active, err := h.languages.Active(ctx, userID); err == nil && active != nil {
		userCEFR = active.CEFRLevel
	}

	// Cache hit on a previously analyzed URL may not yet have the user's
	// current CEFR adaptation if the user changed level between analyses;
	// the renderer regenerates lazily so the card lands fully populated.
	h.render.renderInlineWithNotice(
		ctx, b,
		chatID, msgID,
		loc, userID, userCEFR,
		analyzed.Stored, preview, len(analyzed.Words),
		view, analyzed.Notice, "url",
	)
}

// minutesUntil rounds a "wait this long" duration up to the nearest whole
// minute, with a floor of 1. The user-facing rate-limit message tells the
// user how many minutes are left, and zero is misleading when there are
// still seconds to wait.
func minutesUntil(d time.Duration) int {
	if d <= 0 {
		return 1
	}
	mins := int(math.Ceil(d.Minutes()))
	if mins < 1 {
		mins = 1
	}
	return mins
}

func articleErrorMessageID(err error) string {
	switch {
	case errors.Is(err, articles.ErrNoActiveLanguage):
		return "article.err.no_language"
	case errors.Is(err, articles.ErrNoAPIKey):
		return "article.err.no_api_key"
	case errors.Is(err, articles.ErrNetwork):
		return "article.err.network"
	case errors.Is(err, articles.ErrTooLarge):
		return "article.err.too_large"
	case errors.Is(err, articles.ErrNotArticle):
		return "article.err.not_article"
	case errors.Is(err, articles.ErrPaywall):
		return "article.err.paywall"
	case errors.Is(err, articles.ErrPendingExpired):
		return "article.long.expired"
	case errors.Is(err, articles.ErrBlockedSource):
		return "article.err.blocked_source"
	case errors.Is(err, articles.ErrBlockedContent):
		return "article.err.blocked_content"
	case errors.Is(err, llm.ErrInvalidAPIKey):
		return "apikey.invalid"
	case errors.Is(err, llm.ErrRateLimited):
		return "apikey.rate_limited"
	case errors.Is(err, llm.ErrUnavailable):
		return "apikey.unavailable"
	case errors.Is(err, llm.ErrSchemaInvalid):
		return "article.err.llm_format"
	case errors.Is(err, articles.ErrNoSourceText):
		// Empty adapted_versions on a stored article — treat as a degraded
		// LLM response and surface the same diagnosable message as a
		// schema-invalid one. New analyses can't reach this branch after
		// the schema requires non-empty `adapted_versions.current`, but
		// articles persisted before that fix can.
		return "article.err.llm_format"
	default:
		return "error.generic"
	}
}
