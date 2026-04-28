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

const wordsPageSize = 3

// CallbackPrefixWords drives pagination of the words list opened from the
// article card.
const CallbackPrefixWords = "words:"

// CallbackPrefixWordStatus drives per-word status updates ("Знаю / Учу /
// Пропустить") rendered inside the same paginated message. The payload
// carries article and page so that the message can be re-rendered without
// extra DB lookups: wstat:<article_id>:<page>:<dictionary_word_id>:<status>.
const CallbackPrefixWordStatus = "wstat:"

// Words handles the inline pagination of an article's word list and the
// per-word status buttons (known / learning / skipped) that live in the
// same message.
type Words struct {
	users    *users.Service
	articles articles.Repository
	awords   dictionary.ArticleWordsRepository
	statuses dictionary.UserWordStatusRepository
	bundle   *goi18n.Bundle
	log      *slog.Logger
	db       *sql.DB
}

// NewWords builds the handler. db is used for read queries; status writes
// go through the status repo directly (single-row upserts, no transaction).
func NewWords(
	svc *users.Service,
	articleRepo articles.Repository,
	awords dictionary.ArticleWordsRepository,
	statuses dictionary.UserWordStatusRepository,
	db *sql.DB,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *Words {
	return &Words{
		users:    svc,
		articles: articleRepo,
		awords:   awords,
		statuses: statuses,
		db:       db,
		bundle:   bundle,
		log:      log,
	}
}

// HandleCallback drives the pagination prefix `words:`.
func (h *Words) HandleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	defer func() {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})
	}()

	u, ok := resolveCallbackUser(ctx, h.users, cq, h.log, "words cb")
	if !ok {
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
		return
	}

	articleID, page, err := parseWordsPayload(data)
	if err != nil {
		h.log.Warn("words cb: bad payload", "data", cq.Data, "err", err)
		return
	}
	h.renderPage(ctx, b, u.ID, loc, chatID, msgID, articleID, page)
}

// HandleStatusCallback drives the per-word `wstat:` prefix. It upserts the
// user's status for the word, answers the callback with a short toast, and
// re-renders the page so that the selected button visually reflects the new
// state.
func (h *Words) HandleStatusCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	answered := false
	answer := func(text string) {
		if answered {
			return
		}
		answered = true
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: cq.ID,
			Text:            text,
		})
	}
	defer func() {
		if !answered {
			b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})
		}
	}()

	u, ok := resolveCallbackUser(ctx, h.users, cq, h.log, "wstat cb")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	chatID, msgID, ok := callbackMessageRef(cq)
	if !ok {
		return
	}

	payload := strings.TrimPrefix(cq.Data, CallbackPrefixWordStatus)
	articleID, page, wordID, status, err := parseWordStatusPayload(payload)
	if err != nil {
		h.log.Warn("wstat cb: bad payload", "data", cq.Data, "err", err)
		return
	}

	article, err := h.articles.ByID(ctx, h.db, articleID)
	if err != nil {
		h.log.Warn("wstat cb: article load", "article_id", articleID, "err", err)
		return
	}
	if article.UserID != u.ID {
		h.log.Warn("wstat cb: article ownership mismatch", "article_id", articleID, "user_id", u.ID)
		return
	}

	if err := h.statuses.Upsert(ctx, h.db, dictionary.UserWordStatus{
		UserID:           u.ID,
		DictionaryWordID: wordID,
		Status:           status,
	}); err != nil {
		h.log.Error("wstat cb: upsert", "err", err)
		return
	}

	answer(tgi18n.T(loc, statusConfirmKey(status), nil))
	h.renderPage(ctx, b, u.ID, loc, chatID, msgID, articleID, page)
}

func (h *Words) renderPage(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int, articleID int64, page int) {
	article, err := h.articles.ByID(ctx, h.db, articleID)
	if err != nil {
		h.log.Warn("words render: article load", "article_id", articleID, "err", err)
		return
	}
	if article.UserID != userID {
		h.log.Warn("words render: article ownership mismatch", "article_id", articleID, "user_id", userID)
		return
	}

	total, err := h.awords.CountByArticle(ctx, h.db, articleID)
	if err != nil {
		h.log.Error("words render: count", "err", err)
		return
	}
	if total == 0 {
		b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID: chatID, MessageID: msgID,
			Text:        tgi18n.T(loc, "words.empty", nil),
			ParseMode:   models.ParseModeHTML,
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
		h.log.Error("words render: page", "err", err)
		return
	}

	wordIDs := make([]int64, len(views))
	for i, v := range views {
		wordIDs[i] = v.DictionaryWordID
	}
	statusByWord, err := h.statuses.GetMany(ctx, h.db, userID, wordIDs)
	if err != nil {
		h.log.Error("words render: statuses", "err", err)
		return
	}

	text := renderWordsPage(loc, views, page, totalPages, total)
	markup := wordsKeyboard(loc, articleID, page, totalPages, views, statusByWord)

	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID: chatID, MessageID: msgID,
		Text:        text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: markup,
	}); err != nil {
		h.log.Debug("words render: edit", "err", err)
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

// parseWordStatusPayload decodes `<article_id>:<page>:<word_id>:<status>`.
// Status is restricted to one of the three buttons exposed in the keyboard
// (known / learning / skipped).
func parseWordStatusPayload(s string) (articleID int64, page int, wordID int64, status dictionary.WordStatus, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 4 {
		return 0, 0, 0, "", fmt.Errorf("expected 4 parts, got %d", len(parts))
	}
	aid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, 0, "", fmt.Errorf("article id: %w", err)
	}
	p, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, "", fmt.Errorf("page: %w", err)
	}
	wid, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, 0, 0, "", fmt.Errorf("word id: %w", err)
	}
	st := dictionary.WordStatus(parts[3])
	switch st {
	case dictionary.StatusKnown, dictionary.StatusLearning, dictionary.StatusSkipped:
	default:
		return 0, 0, 0, "", fmt.Errorf("status: %q", parts[3])
	}
	return aid, p, wid, st, nil
}

func statusConfirmKey(s dictionary.WordStatus) string {
	switch s {
	case dictionary.StatusKnown:
		return "wstat.confirm.known"
	case dictionary.StatusLearning:
		return "wstat.confirm.learning"
	case dictionary.StatusSkipped:
		return "wstat.confirm.skipped"
	}
	return "wstat.confirm.learning"
}

// htmlEscape escapes the three characters Telegram's HTML parse mode
// requires: `&`, `<`, `>`. Order matters — `&` must come first.
var htmlEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace

func renderWordsPage(loc *goi18n.Localizer, views []dictionary.ArticleWordView, page, totalPages int, totalWords int) string {
	var sb strings.Builder
	if totalPages > 1 {
		sb.WriteString("<b>")
		sb.WriteString(htmlEscape(tgi18n.T(loc, "words.page_header", map[string]int{"Page": page + 1, "Total": totalPages})))
		sb.WriteString("</b>")
	} else {
		sb.WriteString("<b>")
		sb.WriteString(htmlEscape(tgi18n.T(loc, "words.header_single", map[string]int{"Total": totalWords})))
		sb.WriteString("</b>")
	}
	sb.WriteString("\n\n")
	for i, v := range views {
		fmt.Fprintf(&sb, "<b>%d. %s</b>", i+1, htmlEscape(v.SurfaceForm))
		meta := ""
		if v.Lemma != "" && v.POS != "" {
			meta = fmt.Sprintf("%s, %s", v.Lemma, v.POS)
		} else if v.Lemma != "" {
			meta = v.Lemma
		} else if v.POS != "" {
			meta = v.POS
		}
		if meta != "" {
			fmt.Fprintf(&sb, " · <i>%s</i>", htmlEscape(meta))
		}
		if v.TranscriptionIPA != "" {
			fmt.Fprintf(&sb, " · <code>%s</code>", htmlEscape(v.TranscriptionIPA))
		}
		sb.WriteString("\n")
		if v.TranslationNative != "" {
			fmt.Fprintf(&sb, "→ <b>%s</b>\n", htmlEscape(v.TranslationNative))
		}
		if v.ExampleTarget != "" {
			fmt.Fprintf(&sb, "<i>%s</i>\n", htmlEscape(v.ExampleTarget))
		}
		if v.ExampleNative != "" {
			fmt.Fprintf(&sb, "%s\n", htmlEscape(v.ExampleNative))
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func wordsKeyboard(
	loc *goi18n.Localizer,
	articleID int64,
	page, totalPages int,
	views []dictionary.ArticleWordView,
	statusByWord map[int64]dictionary.WordStatus,
) *models.InlineKeyboardMarkup {
	rows := make([][]models.InlineKeyboardButton, 0, len(views)+2)

	for i, v := range views {
		current := statusByWord[v.DictionaryWordID]
		rows = append(rows, statusRow(loc, articleID, page, i+1, v.DictionaryWordID, current))
	}

	closeText := tgi18n.T(loc, "words.btn.close", nil)

	if totalPages > 1 {
		prev := models.InlineKeyboardButton{Text: tgi18n.T(loc, "words.btn.prev", nil)}
		next := models.InlineKeyboardButton{Text: tgi18n.T(loc, "words.btn.next", nil)}
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
		rows = append(rows, []models.InlineKeyboardButton{prev, next})
	}

	closeBtn := models.InlineKeyboardButton{Text: closeText, CallbackData: CallbackPrefixWords + "close"}
	rows = append(rows, []models.InlineKeyboardButton{closeBtn})

	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func statusRow(loc *goi18n.Localizer, articleID int64, page, index int, wordID int64, current dictionary.WordStatus) []models.InlineKeyboardButton {
	return []models.InlineKeyboardButton{
		statusButton(loc, articleID, page, index, wordID, dictionary.StatusKnown, current, "wstat.btn.known"),
		statusButton(loc, articleID, page, index, wordID, dictionary.StatusLearning, current, "wstat.btn.learning"),
		statusButton(loc, articleID, page, index, wordID, dictionary.StatusSkipped, current, "wstat.btn.skipped"),
	}
}

func statusButton(
	loc *goi18n.Localizer,
	articleID int64,
	page, index int,
	wordID int64,
	status, current dictionary.WordStatus,
	labelKey string,
) models.InlineKeyboardButton {
	label := tgi18n.T(loc, labelKey, map[string]int{"Index": index})
	if current == status {
		label = "✓ " + label
	}
	return models.InlineKeyboardButton{
		Text: label,
		CallbackData: fmt.Sprintf("%s%d:%d:%d:%s",
			CallbackPrefixWordStatus, articleID, page, wordID, status),
	}
}

func closeKeyboard(loc *goi18n.Localizer) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: tgi18n.T(loc, "words.btn.close", nil), CallbackData: CallbackPrefixWords + "close"}},
		},
	}
}
