package handlers

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/articles"
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
	bundle    *goi18n.Bundle
	log       *slog.Logger
}

func NewURL(svc *users.Service, languages users.UserLanguageRepository, articleSvc *articles.Service, bundle *goi18n.Bundle, log *slog.Logger) *URLHandler {
	return &URLHandler{users: svc, languages: languages, articles: articleSvc, bundle: bundle, log: log}
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

	if statusMsg != nil {
		preview := make([]string, 0, len(result.Words))
		for _, w := range result.Words {
			preview = append(preview, w.Lemma)
		}
		view := DefaultCardView()

		userCEFR := ""
		if active, err := h.languages.Active(ctx, u.ID); err == nil && active != nil {
			userCEFR = active.CEFRLevel
		}

		article := result.Article
		// Cache hit on a previously analyzed URL may not yet have the user's
		// current CEFR adaptation if the user changed level between analyses;
		// regen lazily so the card lands fully populated.
		if abs, ok := resolveAbsoluteLevel(userCEFR, view.Level); ok {
			if cur := article.ParseAdaptedVersions(); cur[abs] == "" {
				editStatus(tgi18n.T(loc, "article.regenerating", map[string]string{"Level": abs}))
				if _, err := h.articles.Adapt(ctx, u.ID, article.ID, abs); err != nil {
					h.log.Warn("url: regen", "err", err, "article_id", article.ID, "target", abs)
				} else if reloaded, err := h.articles.ArticleByID(ctx, article.ID); err == nil {
					article = reloaded
				}
			}
		}

		if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:      msg.Chat.ID,
			MessageID:   statusMsg.ID,
			Text:        renderArticleCard(loc, article, userCEFR, preview, len(result.Words), view),
			ReplyMarkup: articleCardKeyboard(loc, article, userCEFR, len(result.Words), view),
		}); err != nil {
			h.log.Debug("url: send card", "err", err)
		}
	}
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
	case errors.Is(err, llm.ErrInvalidAPIKey):
		return "apikey.invalid"
	case errors.Is(err, llm.ErrRateLimited):
		return "apikey.rate_limited"
	case errors.Is(err, llm.ErrUnavailable):
		return "apikey.unavailable"
	case errors.Is(err, llm.ErrSchemaInvalid):
		return "article.err.llm_format"
	default:
		return "error.generic"
	}
}

