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
	"github.com/nikita/tg-linguine/internal/screen"
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
	mgr       *screen.Manager
	users     *users.Service
	languages users.UserLanguageRepository
	articles  articles.Repository
	render    *cardRenderer
	bundle    *goi18n.Bundle
	log       *slog.Logger
	db        *sql.DB
}

func NewHistory(
	mgr *screen.Manager,
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
		mgr:       mgr,
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
	h.showPage(ctx, b, msg.Chat.ID, u.ID, loc, "", 0)
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
		_ = h.mgr.RetireActive(ctx, b, chatID)
		return
	case data == "noop":
		return
	case strings.HasPrefix(data, "f:"):
		// filter+page: hist:f:<category>:<page> ("" category = all).
		body := strings.TrimPrefix(data, "f:")
		parts := strings.SplitN(body, ":", 2)
		if len(parts) != 2 {
			h.log.Warn("history cb: bad filter payload", "data", cq.Data)
			return
		}
		category, ok := decodeCategoryFilter(parts[0])
		if !ok {
			h.log.Warn("history cb: bad category", "data", cq.Data)
			return
		}
		page, err := strconv.Atoi(parts[1])
		if err != nil {
			h.log.Warn("history cb: bad page", "data", cq.Data, "err", err)
			return
		}
		h.showPage(ctx, b, chatID, u.ID, loc, category, page)
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

// showPage builds a page and renders it via mgr.Show. Same screen ID means
// Manager will edit in place for pagination callbacks.
func (h *History) showPage(ctx context.Context, b *bot.Bot, chatID, userID int64, loc *goi18n.Localizer, category string, page int) {
	text, markup, ok := h.buildPage(ctx, userID, loc, category, page)
	if !ok {
		_ = h.mgr.Show(ctx, b, chatID, screen.Screen{
			ID:       screen.ScreenHistory,
			Text:     tgi18n.T(loc, "error.generic", nil),
			Keyboard: screen.WithNavigation(loc, nil, "", nil),
		})
		return
	}
	kb := screen.WithNavigation(loc, markup, "", nil)
	_ = h.mgr.Show(ctx, b, chatID, screen.Screen{
		ID:       screen.ScreenHistory,
		Text:     text,
		Keyboard: kb,
		Context:  map[string]string{"f": encodeCategoryFilter(category), "p": strconv.Itoa(page)},
	})
}

// RenderForChat is called by the nav renderer to re-render the history screen
// when the user navigates back to it.
func (h *History) RenderForChat(ctx context.Context, b *bot.Bot, chatID int64, sctx map[string]string) {
	u, err := h.users.ByTelegramID(ctx, chatID)
	if err != nil {
		h.log.Warn("history render: user lookup", "chat_id", chatID, "err", err)
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	cat, _ := decodeCategoryFilter(sctx["f"])
	page, _ := strconv.Atoi(sctx["p"])
	h.showPage(ctx, b, chatID, u.ID, loc, cat, page)
}

func (h *History) buildPage(ctx context.Context, userID int64, loc *goi18n.Localizer, category string, page int) (string, *models.InlineKeyboardMarkup, bool) {
	total, err := h.articles.CountByUserAndCategory(ctx, h.db, userID, category)
	if err != nil {
		h.log.Error("history: count", "err", err)
		return "", nil, false
	}
	if total == 0 {
		// When the user has *zero* articles overall, show the onboarding
		// message; under a category filter that just has no matches, keep
		// the category buttons available so they can switch back to All.
		if category == "" {
			return tgi18n.T(loc, "history.empty", nil), historyEmptyKeyboard(), true
		}
		header := tgi18n.T(loc, "history.empty_category", map[string]string{
			"Category": tgi18n.T(loc, "category."+category, nil),
		})
		return header, historyFilterOnlyKeyboard(loc, category), true
	}

	totalPages := (total + historyPageSize - 1) / historyPageSize
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	rows, err := h.articles.ListByUserAndCategory(ctx, h.db, userID, category, historyPageSize, page*historyPageSize)
	if err != nil {
		h.log.Error("history: list", "err", err)
		return "", nil, false
	}

	header := tgi18n.T(loc, "history.header", map[string]any{
		"Page":  page + 1,
		"Total": totalPages,
	})
	if category != "" {
		header += " · " + tgi18n.T(loc, "category."+category, nil)
	}
	return header, historyKeyboard(loc, rows, category, page, totalPages), true
}

func historyKeyboard(loc *goi18n.Localizer, rows []articles.Article, category string, page, totalPages int) *models.InlineKeyboardMarkup {
	out := make([][]models.InlineKeyboardButton, 0, len(rows)+5)
	out = append(out, historyCategoryRows(loc, category)...)
	for _, a := range rows {
		out = append(out, []models.InlineKeyboardButton{{
			Text:         historyEntryLabel(a),
			CallbackData: fmt.Sprintf("%sopen:%d", CallbackPrefixHistory, a.ID),
		}})
	}

	prev := models.InlineKeyboardButton{Text: tgi18n.T(loc, "history.btn.prev", nil)}
	next := models.InlineKeyboardButton{Text: tgi18n.T(loc, "history.btn.next", nil)}
	if page > 0 {
		prev.CallbackData = historyPageCallback(category, page-1)
	} else {
		prev.CallbackData = CallbackPrefixHistory + "noop"
	}
	if page < totalPages-1 {
		next.CallbackData = historyPageCallback(category, page+1)
	} else {
		next.CallbackData = CallbackPrefixHistory + "noop"
	}
	out = append(out, []models.InlineKeyboardButton{prev, next})

	return &models.InlineKeyboardMarkup{InlineKeyboard: out}
}

func historyEmptyKeyboard() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{},
	}
}

func historyFilterOnlyKeyboard(loc *goi18n.Localizer, category string) *models.InlineKeyboardMarkup {
	out := historyCategoryRows(loc, category)
	return &models.InlineKeyboardMarkup{InlineKeyboard: out}
}

// historyCategoryRows lays out the All + per-category filter buttons on
// three rows. The active filter is marked with "✓ " so the user sees which
// view they are currently in.
func historyCategoryRows(loc *goi18n.Localizer, current string) [][]models.InlineKeyboardButton {
	all := append([]string{""}, articles.CategoryCodes...) // "" = All
	var rows [][]models.InlineKeyboardButton
	const perRow = 4
	for i := 0; i < len(all); i += perRow {
		end := i + perRow
		if end > len(all) {
			end = len(all)
		}
		row := make([]models.InlineKeyboardButton, 0, end-i)
		for _, code := range all[i:end] {
			label := historyCategoryLabel(loc, code)
			if code == current {
				label = "✓ " + label
			}
			row = append(row, models.InlineKeyboardButton{
				Text:         label,
				CallbackData: historyPageCallback(code, 0),
			})
		}
		rows = append(rows, row)
	}
	return rows
}

func historyCategoryLabel(loc *goi18n.Localizer, code string) string {
	if code == "" {
		return tgi18n.T(loc, "history.btn.cat_all", nil)
	}
	return tgi18n.T(loc, "category."+code, nil)
}

func historyPageCallback(category string, page int) string {
	return fmt.Sprintf("%sf:%s:%d", CallbackPrefixHistory, encodeCategoryFilter(category), page)
}

// encodeCategoryFilter / decodeCategoryFilter swap the empty string for "_"
// in the wire payload so the colon-separated callback always has the same
// shape (`f:<token>:<page>`).
func encodeCategoryFilter(c string) string {
	if c == "" {
		return "_"
	}
	return c
}

func decodeCategoryFilter(token string) (string, bool) {
	if token == "_" {
		return "", true
	}
	if articles.IsCategoryCode(token) {
		return token, true
	}
	return "", false
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
