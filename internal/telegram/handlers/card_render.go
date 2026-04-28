package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/go-telegram/bot"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/dictionary"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
)

// cardRenderer collects the dependencies needed to (re)open a stored article
// card: ensure the user-CEFR adaptation is present (regenerating via the LLM
// if missing), then edit the supplied message to display the rendered card.
//
// Three handlers share this flow — `/history`'s open callback, the `art:`
// view-switch callback, and the URL pipeline's cache-hit branch — so the
// regen-then-render piece lives here once instead of being copied into each.
//
// `*bot.Bot` is passed per-call (not stored) because Telegram dispatches it
// into each handler invocation.
type cardRenderer struct {
	log      *slog.Logger
	articles articles.Repository
	awords   dictionary.ArticleWordsRepository
	regen    CardRegenerator
	db       *sql.DB
}

func newCardRenderer(
	log *slog.Logger,
	articleRepo articles.Repository,
	awords dictionary.ArticleWordsRepository,
	regen CardRegenerator,
	db *sql.DB,
) *cardRenderer {
	return &cardRenderer{
		log:      log,
		articles: articleRepo,
		awords:   awords,
		regen:    regen,
		db:       db,
	}
}

// renderInline renders an article whose preview/word-count are already known
// to the caller (e.g. fresh from `AnalyzeArticle`). Ownership is the caller's
// responsibility. On regen failure it edits the message to a localized
// error and returns without rendering the card.
func (r *cardRenderer) renderInline(
	ctx context.Context,
	b *bot.Bot,
	chatID any, msgID int,
	loc *goi18n.Localizer,
	userID int64, userCEFR string,
	article *articles.Article,
	preview []string,
	totalWords int,
	view ArticleView,
	logPrefix string,
) {
	r.renderInlineWithNotice(ctx, b, chatID, msgID, loc, userID, userCEFR, article, preview, totalWords, view, "", logPrefix)
}

// renderInlineWithNotice is the renderInline variant used by the
// long-article fallback paths. The notice string (when non-empty) is
// prepended to the card text as a single-line banner so the user sees that
// the analysis ran on a transformed body.
func (r *cardRenderer) renderInlineWithNotice(
	ctx context.Context,
	b *bot.Bot,
	chatID any, msgID int,
	loc *goi18n.Localizer,
	userID int64, userCEFR string,
	article *articles.Article,
	preview []string,
	totalWords int,
	view ArticleView,
	notice string,
	logPrefix string,
) {
	article, ok := r.ensureAdapted(ctx, b, chatID, msgID, loc, userID, userCEFR, article, view, logPrefix)
	if !ok {
		return
	}
	r.editCardWithNotice(ctx, b, chatID, msgID, loc, article, userCEFR, preview, totalWords, view, notice, logPrefix)
}

// openByID is the repo-backed variant: it loads the article, checks
// ownership, fetches the preview lemmas, then runs the same regen-render
// pipeline as renderInline. Used by `/history` open and the `art:` callback.
func (r *cardRenderer) openByID(
	ctx context.Context,
	b *bot.Bot,
	chatID any, msgID int,
	loc *goi18n.Localizer,
	userID int64, userCEFR string,
	articleID int64,
	view ArticleView,
	logPrefix string,
) {
	article, err := r.articles.ByID(ctx, r.db, articleID)
	if err != nil {
		if errors.Is(err, articles.ErrNotFound) {
			r.log.Warn(logPrefix+": not found", "article_id", articleID)
		} else {
			r.log.Error(logPrefix+": load", "err", err)
		}
		return
	}
	if article.UserID != userID {
		r.log.Warn(logPrefix+": ownership mismatch", "article_id", articleID, "user_id", userID)
		return
	}

	totalWords, err := r.awords.CountByArticle(ctx, r.db, articleID)
	if err != nil {
		r.log.Error(logPrefix+": count", "err", err)
		return
	}
	var preview []string
	if totalWords > 0 {
		views, err := r.awords.PageByArticle(ctx, r.db, articleID, articleCardPreviewLimit, 0)
		if err != nil {
			r.log.Error(logPrefix+": preview", "err", err)
			return
		}
		preview = make([]string, 0, len(views))
		for _, v := range views {
			preview = append(preview, v.Lemma)
		}
	}

	r.renderInline(ctx, b, chatID, msgID, loc, userID, userCEFR, article, preview, totalWords, view, logPrefix)
}

// ensureAdapted regenerates the per-level adaptation if the requested view
// level is not yet present in adapted_versions, edits the message to a
// "generating…" status while the LLM call is in flight, and reloads the
// article record so the caller sees the merged JSON. Returns (nil, false)
// when the regen call fails — in that case the message has already been
// edited to a localized error.
func (r *cardRenderer) ensureAdapted(
	ctx context.Context,
	b *bot.Bot,
	chatID any, msgID int,
	loc *goi18n.Localizer,
	userID int64, userCEFR string,
	article *articles.Article,
	view ArticleView,
	logPrefix string,
) (*articles.Article, bool) {
	if r.regen == nil {
		return article, true
	}
	abs, ok := resolveAbsoluteLevel(userCEFR, view.Level)
	if !ok {
		return article, true
	}
	if article.ParseAdaptedVersions()[abs] != "" {
		return article, true
	}

	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: msgID,
		Text:      tgi18n.T(loc, "article.regenerating", map[string]string{"Level": abs}),
	}); err != nil {
		r.log.Debug(logPrefix+": edit status", "err", err)
	}
	if _, err := r.regen.Adapt(ctx, userID, article.ID, abs); err != nil {
		r.log.Warn(logPrefix+": regen", "err", err, "article_id", article.ID, "target", abs)
		if _, editErr := b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: msgID,
			Text:      tgi18n.T(loc, articleErrorMessageID(err), nil),
		}); editErr != nil {
			r.log.Debug(logPrefix+": edit error", "err", editErr)
		}
		return nil, false
	}
	reloaded, err := r.articles.ByID(ctx, r.db, article.ID)
	if err != nil {
		r.log.Error(logPrefix+": reload after regen", "err", err)
		return nil, false
	}
	return reloaded, true
}

func (r *cardRenderer) editCard(
	ctx context.Context,
	b *bot.Bot,
	chatID any, msgID int,
	loc *goi18n.Localizer,
	article *articles.Article,
	userCEFR string,
	preview []string,
	totalWords int,
	view ArticleView,
	logPrefix string,
) {
	r.editCardWithNotice(ctx, b, chatID, msgID, loc, article, userCEFR, preview, totalWords, view, "", logPrefix)
}

func (r *cardRenderer) editCardWithNotice(
	ctx context.Context,
	b *bot.Bot,
	chatID any, msgID int,
	loc *goi18n.Localizer,
	article *articles.Article,
	userCEFR string,
	preview []string,
	totalWords int,
	view ArticleView,
	notice string,
	logPrefix string,
) {
	text := renderArticleCard(loc, article, userCEFR, preview, totalWords, view)
	if notice != "" {
		text = notice + "\n\n" + text
	}
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        text,
		ReplyMarkup: articleCardKeyboard(loc, article, userCEFR, totalWords, view),
	}); err != nil {
		r.log.Debug(logPrefix+": edit", "err", err)
	}
}
