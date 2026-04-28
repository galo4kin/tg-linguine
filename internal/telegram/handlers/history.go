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

const (
	historyPageSize = 10
	// historyTitleMax caps the per-row button label so Telegram's 64-char
	// callback budget and the visual layout stay sane on small screens.
	historyTitleMax = 48
)

// CallbackPrefixHistory drives both list pagination (`hist:page:<n>`),
// opening a stored card (`hist:open:<article_id>`), and dismissing the list
// (`hist:close`).
const CallbackPrefixHistory = "hist:"

// History serves the `/history` command and the `hist:` callback family. It
// reuses the article-card renderer so the reopened card matches what the user
// saw right after the original analysis (without re-calling the LLM).
type History struct {
	users     *users.Service
	languages users.UserLanguageRepository
	articles  articles.Repository
	render    *cardRenderer
	bundle    *goi18n.Bundle
	log       *slog.Logger
	db        *sql.DB
}

func NewHistory(
	svc *users.Service,
	languages users.UserLanguageRepository,
	articleRepo articles.Repository,
	awords dictionary.ArticleWordsRepository,
	regen CardRegenerator,
	db *sql.DB,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *History {
	return &History{
		users:     svc,
		languages: languages,
		articles:  articleRepo,
		render:    newCardRenderer(log, articleRepo, awords, regen, db),
		bundle:    bundle,
		log:       log,
		db:        db,
	}
}

// HandleCommand reacts to the `/history` text command — sends a fresh first
// page of stored articles.
func (h *History) HandleCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}
	u, ok := resolveMessageUser(ctx, h.users, msg, h.log, "history cmd")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	h.sendPage(ctx, b, msg.Chat.ID, u.ID, loc, 0)
}

// HandleCallback drives the `hist:` prefix.
func (h *History) HandleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	defer func() {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})
	}()

	u, ok := resolveCallbackUser(ctx, h.users, cq, h.log, "history cb")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	chatID, msgID, ok := callbackMessageRef(cq)
	if !ok {
		return
	}

	data := strings.TrimPrefix(cq.Data, CallbackPrefixHistory)
	switch {
	case data == "close":
		b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: msgID})
		return
	case data == "noop":
		return
	case strings.HasPrefix(data, "page:"):
		page, err := strconv.Atoi(strings.TrimPrefix(data, "page:"))
		if err != nil {
			h.log.Warn("history cb: bad page", "data", cq.Data, "err", err)
			return
		}
		h.editPage(ctx, b, chatID, msgID, u.ID, loc, page)
	case strings.HasPrefix(data, "open:"):
		idStr := strings.TrimPrefix(data, "open:")
		articleID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			h.log.Warn("history cb: bad article id", "data", cq.Data, "err", err)
			return
		}
		userCEFR := ""
		if active, err := h.languages.Active(ctx, u.ID); err == nil && active != nil {
			userCEFR = active.CEFRLevel
		}
		h.render.openByID(ctx, b, chatID, msgID, loc, u.ID, userCEFR, articleID, DefaultCardView(), "history open")
	default:
		h.log.Warn("history cb: unknown payload", "data", cq.Data)
	}
}

func (h *History) sendPage(ctx context.Context, b *bot.Bot, chatID, userID int64, loc *goi18n.Localizer, page int) {
	text, markup, ok := h.buildPage(ctx, userID, loc, page)
	if !ok {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   tgi18n.T(loc, "error.generic", nil),
		})
		return
	}
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: markup,
	})
}

func (h *History) editPage(ctx context.Context, b *bot.Bot, chatID any, msgID int, userID int64, loc *goi18n.Localizer, page int) {
	text, markup, ok := h.buildPage(ctx, userID, loc, page)
	if !ok {
		return
	}
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        text,
		ReplyMarkup: markup,
	}); err != nil {
		h.log.Debug("history edit: edit", "err", err)
	}
}

func (h *History) buildPage(ctx context.Context, userID int64, loc *goi18n.Localizer, page int) (string, *models.InlineKeyboardMarkup, bool) {
	total, err := h.articles.CountByUser(ctx, h.db, userID)
	if err != nil {
		h.log.Error("history: count", "err", err)
		return "", nil, false
	}
	if total == 0 {
		return tgi18n.T(loc, "history.empty", nil), historyEmptyKeyboard(loc), true
	}

	totalPages := (total + historyPageSize - 1) / historyPageSize
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	rows, err := h.articles.ListByUser(ctx, h.db, userID, historyPageSize, page*historyPageSize)
	if err != nil {
		h.log.Error("history: list", "err", err)
		return "", nil, false
	}

	header := tgi18n.T(loc, "history.header", map[string]int{"Page": page + 1, "Total": totalPages})
	return header, historyKeyboard(loc, rows, page, totalPages), true
}

func historyKeyboard(loc *goi18n.Localizer, rows []articles.Article, page, totalPages int) *models.InlineKeyboardMarkup {
	out := make([][]models.InlineKeyboardButton, 0, len(rows)+2)
	for _, a := range rows {
		out = append(out, []models.InlineKeyboardButton{{
			Text:         historyEntryLabel(a),
			CallbackData: fmt.Sprintf("%sopen:%d", CallbackPrefixHistory, a.ID),
		}})
	}

	prev := models.InlineKeyboardButton{Text: tgi18n.T(loc, "history.btn.prev", nil)}
	next := models.InlineKeyboardButton{Text: tgi18n.T(loc, "history.btn.next", nil)}
	if page > 0 {
		prev.CallbackData = fmt.Sprintf("%spage:%d", CallbackPrefixHistory, page-1)
	} else {
		prev.CallbackData = CallbackPrefixHistory + "noop"
	}
	if page < totalPages-1 {
		next.CallbackData = fmt.Sprintf("%spage:%d", CallbackPrefixHistory, page+1)
	} else {
		next.CallbackData = CallbackPrefixHistory + "noop"
	}
	closeBtn := models.InlineKeyboardButton{
		Text:         tgi18n.T(loc, "history.btn.close", nil),
		CallbackData: CallbackPrefixHistory + "close",
	}
	out = append(out, []models.InlineKeyboardButton{prev, next})
	out = append(out, []models.InlineKeyboardButton{closeBtn})

	return &models.InlineKeyboardMarkup{InlineKeyboard: out}
}

func historyEmptyKeyboard(loc *goi18n.Localizer) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{
				Text:         tgi18n.T(loc, "history.btn.close", nil),
				CallbackData: CallbackPrefixHistory + "close",
			}},
		},
	}
}

func historyEntryLabel(a articles.Article) string {
	date := a.CreatedAt.Format("2006-01-02")
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = a.SourceURL
	}
	if r := []rune(title); len(r) > historyTitleMax {
		title = string(r[:historyTitleMax-1]) + "…"
	}
	return date + " · " + title
}
