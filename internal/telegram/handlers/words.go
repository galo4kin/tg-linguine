package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/dictionary"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/users"
)

const wordsPageSize = 5

// Words handles the inline pagination of an article's word list, opened
// from the article card's "Show all words" button.
type Words struct {
	users    *users.Service
	articles articles.Repository
	awords   dictionary.ArticleWordsRepository
	bundle   *goi18n.Bundle
	log      *slog.Logger
	db       *sql.DB
}

// NewWords builds the handler. db is used for read queries; writes (which
// the pagination handler does not perform) go through the repos themselves.
func NewWords(
	svc *users.Service,
	articleRepo articles.Repository,
	awords dictionary.ArticleWordsRepository,
	db *sql.DB,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *Words {
	return &Words{users: svc, articles: articleRepo, awords: awords, db: db, bundle: bundle, log: log}
}

func (h *Words) HandleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}

	// Always answer the callback to dismiss the loading spinner on the client.
	defer func() {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})
	}()

	from := cq.From
	u, _, err := h.users.RegisterUser(ctx, users.TelegramUser{
		ID: from.ID, Username: from.Username, FirstName: from.FirstName, LanguageCode: from.LanguageCode,
	})
	if err != nil {
		h.log.Error("words cb: register", "err", err)
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)

	chatID, msgID, ok := callbackMessageRef(cq)
	if !ok {
		return
	}
	data := strings.TrimPrefix(cq.Data, CallbackPrefixWords)

	if data == "close" {
		b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: msgID})
		return
	}
	if data == "noop" {
		// Disabled-edge buttons; just dismiss the spinner (handled by deferred answer).
		return
	}

	articleID, page, err := parseWordsPayload(data)
	if err != nil {
		h.log.Warn("words cb: bad payload", "data", cq.Data, "err", err)
		return
	}

	// Verify the article belongs to the user.
	article, err := h.articles.ByID(ctx, h.db, articleID)
	if err != nil {
		h.log.Warn("words cb: article load", "article_id", articleID, "err", err)
		return
	}
	if article.UserID != u.ID {
		h.log.Warn("words cb: article ownership mismatch", "article_id", articleID, "user_id", u.ID)
		return
	}

	total, err := h.awords.CountByArticle(ctx, h.db, articleID)
	if err != nil {
		h.log.Error("words cb: count", "err", err)
		return
	}
	if total == 0 {
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID: chatID, MessageID: msgID,
			Text:        tgi18n.T(loc, "words.empty", nil),
			ReplyMarkup: closeKeyboard(loc),
		})
		return
	}

	totalPages := (total + wordsPageSize - 1) / wordsPageSize
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	views, err := h.awords.PageByArticle(ctx, h.db, articleID, wordsPageSize, page*wordsPageSize)
	if err != nil {
		h.log.Error("words cb: page", "err", err)
		return
	}

	text := renderWordsPage(loc, views, page, totalPages)
	markup := wordsKeyboard(loc, articleID, page, totalPages)

	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: chatID, MessageID: msgID,
		Text:        text,
		ReplyMarkup: markup,
	}); err != nil {
		h.log.Debug("words cb: edit", "err", err)
	}
}

func parseWordsPayload(s string) (articleID int64, page int, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected 2 parts, got %d", len(parts))
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("article id: %w", err)
	}
	p, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("page: %w", err)
	}
	return id, p, nil
}

func renderWordsPage(loc *goi18n.Localizer, views []dictionary.ArticleWordView, page, totalPages int) string {
	var sb strings.Builder
	sb.WriteString(tgi18n.T(loc, "words.page_header", map[string]int{"Page": page + 1, "Total": totalPages}))
	sb.WriteString("\n\n")
	for _, v := range views {
		fmt.Fprintf(&sb, "• %s (%s, %s) [%s]\n", v.SurfaceForm, v.Lemma, v.POS, v.TranscriptionIPA)
		if v.TranslationNative != "" {
			fmt.Fprintf(&sb, "  → %s\n", v.TranslationNative)
		}
		if v.ExampleTarget != "" {
			fmt.Fprintf(&sb, "  %s\n", v.ExampleTarget)
		}
		if v.ExampleNative != "" {
			fmt.Fprintf(&sb, "  %s\n", v.ExampleNative)
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func wordsKeyboard(loc *goi18n.Localizer, articleID int64, page, totalPages int) *models.InlineKeyboardMarkup {
	prevText := tgi18n.T(loc, "words.btn.prev", nil)
	nextText := tgi18n.T(loc, "words.btn.next", nil)
	closeText := tgi18n.T(loc, "words.btn.close", nil)

	prev := models.InlineKeyboardButton{Text: prevText}
	next := models.InlineKeyboardButton{Text: nextText}
	if page > 0 {
		prev.CallbackData = fmt.Sprintf("%s%d:%d", CallbackPrefixWords, articleID, page-1)
	} else {
		prev.CallbackData = CallbackPrefixWords + "noop"
	}
	if page < totalPages-1 {
		next.CallbackData = fmt.Sprintf("%s%d:%d", CallbackPrefixWords, articleID, page+1)
	} else {
		next.CallbackData = CallbackPrefixWords + "noop"
	}
	closeBtn := models.InlineKeyboardButton{Text: closeText, CallbackData: CallbackPrefixWords + "close"}

	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{prev, next},
			{closeBtn},
		},
	}
}

func closeKeyboard(loc *goi18n.Localizer) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: tgi18n.T(loc, "words.btn.close", nil), CallbackData: CallbackPrefixWords + "close"}},
		},
	}
}
