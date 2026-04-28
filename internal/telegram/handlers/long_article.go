package handlers

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/dictionary"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/users"
)

// LongArticleHandler routes the `lng:<id>:<variant>` callback emitted by
// the URL handler when an article exceeded the per-request token budget.
// Variants: `t` for paragraph-truncate, `s` for LLM pre-summary.
type LongArticleHandler struct {
	users     *users.Service
	languages users.UserLanguageRepository
	articles  *articles.Service
	render    *cardRenderer
	bundle    *goi18n.Bundle
	log       *slog.Logger
}

// NewLongArticle wires the handler with the same repos the URL handler
// uses for rendering the resulting article card.
func NewLongArticle(
	svc *users.Service,
	languages users.UserLanguageRepository,
	articleSvc *articles.Service,
	articleRepo articles.Repository,
	awords dictionary.ArticleWordsRepository,
	db *sql.DB,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *LongArticleHandler {
	return &LongArticleHandler{
		users:     svc,
		languages: languages,
		articles:  articleSvc,
		render:    newCardRenderer(log, articleRepo, awords, articleSvc, db),
		bundle:    bundle,
		log:       log,
	}
}

// HandleCallback is registered for the CallbackPrefixLongArticle prefix.
func (h *LongArticleHandler) HandleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	defer b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})

	u, ok := resolveCallbackUser(ctx, h.users, cq, h.log, "long")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)

	chatID, msgID, ok := callbackMessageRef(cq)
	if !ok {
		return
	}

	pendingID, mode, ok := parseLongArticleCallback(cq.Data)
	if !ok {
		h.log.Debug("long: unparsable callback", "data", cq.Data)
		return
	}

	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: msgID,
		Text:      tgi18n.T(loc, "article.analyzing", nil),
	}); err != nil {
		h.log.Debug("long: edit status", "err", err)
	}

	notice := &localizedNoticeRenderer{loc: loc}
	analyzed, err := h.articles.AnalyzeExtracted(ctx, u.ID, pendingID, mode, notice, nil)
	if err != nil {
		h.log.Warn("long: analyze failed", "user_id", u.ID, "err", err)
		if _, editErr := b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: msgID,
			Text:      tgi18n.T(loc, articleErrorMessageID(err), nil),
		}); editErr != nil {
			h.log.Debug("long: edit error", "err", editErr)
		}
		return
	}

	preview := make([]string, 0, len(analyzed.Words))
	for _, w := range analyzed.Words {
		preview = append(preview, w.Lemma)
	}
	view := DefaultCardView()

	userCEFR := ""
	if active, err := h.languages.Active(ctx, u.ID); err == nil && active != nil {
		userCEFR = active.CEFRLevel
	}

	h.render.renderInlineWithNotice(
		ctx, b,
		chatID, msgID,
		loc, u.ID, userCEFR,
		analyzed.Article, preview, len(analyzed.Words),
		view, analyzed.Notice, "long",
	)
}

// parseLongArticleCallback decodes a `lng:<id>:<variant>` payload.
// Returns (id, mode, true) on success.
func parseLongArticleCallback(data string) (string, articles.LongAnalysisMode, bool) {
	rest := strings.TrimPrefix(data, CallbackPrefixLongArticle)
	if rest == data {
		return "", 0, false
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 2 || parts[0] == "" {
		return "", 0, false
	}
	switch parts[1] {
	case "t":
		return parts[0], articles.ModeTruncate, true
	case "s":
		return parts[0], articles.ModeSummarize, true
	default:
		return "", 0, false
	}
}

// localizedNoticeRenderer plugs the i18n bundle into the article service so
// the service layer can emit a banner string in the user's language without
// importing the i18n package directly.
type localizedNoticeRenderer struct {
	loc *goi18n.Localizer
}

func (r *localizedNoticeRenderer) RenderNotice(d articles.NoticeData) string {
	switch d.Kind {
	case articles.NoticeTruncated:
		return tgi18n.T(r.loc, "article.long.banner.truncated", map[string]int{
			"Percent":    d.Percent,
			"Words":      d.Words,
			"TotalWords": d.TotalWords,
		})
	case articles.NoticeSummarized:
		return tgi18n.T(r.loc, "article.long.banner.summarized", map[string]int{
			"TotalWords": d.TotalWords,
		})
	default:
		return ""
	}
}
