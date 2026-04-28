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

// Card serves the `art:` callback family — adapted-level switches and the
// summary-language toggle, both rendered through EditMessageText so the
// inline message stays in place.
type Card struct {
	users     *users.Service
	languages users.UserLanguageRepository
	render    *cardRenderer
	bundle    *goi18n.Bundle
	log       *slog.Logger
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
		render:    newCardRenderer(log, articleRepo, awords, regen, db),
		bundle:    bundle,
		log:       log,
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

	userCEFR := ""
	if active, err := h.languages.Active(ctx, u.ID); err == nil && active != nil {
		userCEFR = active.CEFRLevel
	}

	h.render.openByID(ctx, b, chatID, msgID, loc, u.ID, userCEFR, articleID, view, "card cb")
}
