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
	users     *users.Service
	languages users.UserLanguageRepository
	articles  articles.Repository
	awords    dictionary.ArticleWordsRepository
	regen     CardRegenerator
	bundle    *goi18n.Bundle
	log       *slog.Logger
	db        *sql.DB
}

// CardRegenerator is the slice of *articles.Service the card handler needs —
// kept narrow so the test stub does not have to implement the full pipeline.
type CardRegenerator interface {
	Adapt(ctx context.Context, userID, articleID int64, targetLevel string) (string, error)
}

func NewCard(
	svc *users.Service,
	languages users.UserLanguageRepository,
	articleRepo articles.Repository,
	awords dictionary.ArticleWordsRepository,
	regen CardRegenerator,
	db *sql.DB,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *Card {
	return &Card{
		users:     svc,
		languages: languages,
		articles:  articleRepo,
		awords:    awords,
		regen:     regen,
		bundle:    bundle,
		log:       log,
		db:        db,
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

	userCEFR := ""
	if active, err := h.languages.Active(ctx, u.ID); err == nil && active != nil {
		userCEFR = active.CEFRLevel
	}

	// If the requested level isn't yet in adapted_versions, regenerate it
	// via the LLM mini-prompt before re-rendering the card.
	if abs, ok := resolveAbsoluteLevel(userCEFR, view.Level); ok {
		if cur := article.ParseAdaptedVersions(); cur[abs] == "" {
			article = h.regenerateAndReload(ctx, b, chatID, msgID, loc, abs, u.ID, articleID, article)
			if article == nil {
				return
			}
		}
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

	text := renderArticleCard(loc, article, userCEFR, preview, totalWords, view)
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        text,
		ReplyMarkup: articleCardKeyboard(loc, article, userCEFR, totalWords, view),
	}); err != nil {
		h.log.Debug("card cb: edit", "err", err)
	}
}

// regenerateAndReload edits the open message to a "generating" status,
// invokes the LLM mini-call to fill in the missing per-level adaptation,
// then reloads the article record so the freshly merged JSON is reflected
// in the next render. On error it edits the message back to a friendly
// localized error and returns nil so the caller bails.
func (h *Card) regenerateAndReload(
	ctx context.Context,
	b *bot.Bot,
	chatID any,
	msgID int,
	loc *goi18n.Localizer,
	targetLevel string,
	userID, articleID int64,
	article *articles.Article,
) *articles.Article {
	if h.regen == nil {
		return article
	}
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: msgID,
		Text:      tgi18n.T(loc, "article.regenerating", map[string]string{"Level": targetLevel}),
	}); err != nil {
		h.log.Debug("card cb: edit status", "err", err)
	}
	if _, err := h.regen.Adapt(ctx, userID, articleID, targetLevel); err != nil {
		h.log.Warn("card cb: regen", "err", err, "article_id", articleID, "target", targetLevel)
		if _, editErr := b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: msgID,
			Text:      tgi18n.T(loc, articleErrorMessageID(err), nil),
		}); editErr != nil {
			h.log.Debug("card cb: edit error", "err", editErr)
		}
		return nil
	}
	reloaded, err := h.articles.ByID(ctx, h.db, articleID)
	if err != nil {
		h.log.Error("card cb: reload after regen", "err", err)
		return nil
	}
	return reloaded
}
