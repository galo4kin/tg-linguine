package handlers

import (
	"context"
	"database/sql"
	"errors"
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

// Card serves the `art:` callback family — adapted-level switches and the
// summary-language toggle, both rendered through EditMessageText so the
// inline message stays in place.
type Card struct {
	users    *users.Service
	articles articles.Repository
	awords   dictionary.ArticleWordsRepository
	bundle   *goi18n.Bundle
	log      *slog.Logger
	db       *sql.DB
}

func NewCard(
	svc *users.Service,
	articleRepo articles.Repository,
	awords dictionary.ArticleWordsRepository,
	db *sql.DB,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *Card {
	return &Card{
		users:    svc,
		articles: articleRepo,
		awords:   awords,
		bundle:   bundle,
		log:      log,
		db:       db,
	}
}

// HandleCallback drives the `art:` prefix.
func (h *Card) HandleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	defer func() {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})
	}()

	payload := strings.TrimPrefix(cq.Data, CallbackPrefixCard)
	if payload == "noop" {
		return
	}
	articleID, view, ok := parseCardCallback(payload)
	if !ok {
		h.log.Warn("card cb: bad payload", "data", cq.Data)
		return
	}

	u, ok := resolveCallbackUser(ctx, h.users, cq, h.log, "card cb")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	chatID, msgID, ok := callbackMessageRef(cq)
	if !ok {
		return
	}

	article, err := h.articles.ByID(ctx, h.db, articleID)
	if err != nil {
		if errors.Is(err, articles.ErrNotFound) {
			h.log.Warn("card cb: not found", "article_id", articleID)
		} else {
			h.log.Error("card cb: load", "err", err)
		}
		return
	}
	if article.UserID != u.ID {
		h.log.Warn("card cb: ownership mismatch", "article_id", articleID, "user_id", u.ID)
		return
	}

	totalWords, err := h.awords.CountByArticle(ctx, h.db, articleID)
	if err != nil {
		h.log.Error("card cb: count", "err", err)
		return
	}
	var preview []string
	if totalWords > 0 {
		views, err := h.awords.PageByArticle(ctx, h.db, articleID, articleCardPreviewLimit, 0)
		if err != nil {
			h.log.Error("card cb: preview", "err", err)
			return
		}
		preview = make([]string, 0, len(views))
		for _, v := range views {
			preview = append(preview, v.Lemma)
		}
	}

	text := renderArticleCard(loc, article, preview, totalWords, view)
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        text,
		ReplyMarkup: articleCardKeyboard(loc, article, totalWords, view),
	}); err != nil {
		h.log.Debug("card cb: edit", "err", err)
	}
}
