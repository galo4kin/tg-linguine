package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/dictionary"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/users"
)

const myWordsPageSize = 10

// CallbackPrefixMyWords drives the /mywords inline menu. Payload shapes:
//
//	mw:f:<filter>:<page>           — render the list with `filter` at `page`
//	mw:e:<filter>:<page>:<word_id> — open the per-word edit submenu
//	mw:s:<filter>:<page>:<word_id>:<status> — apply a status; back to list
//	mw:close                       — dismiss the message
//	mw:noop                        — disabled-button placeholder
//
// Filter codes: a=all (learning|known|mastered), l=learning, k=known,
// m=mastered. Skipped words are intentionally hidden from /mywords and
// from the "All" filter — skipped means "not part of my vocabulary".
const CallbackPrefixMyWords = "mw:"

type myWordsFilter byte

const (
	myWordsFilterAll      myWordsFilter = 'a'
	myWordsFilterLearning myWordsFilter = 'l'
	myWordsFilterKnown    myWordsFilter = 'k'
	myWordsFilterMastered myWordsFilter = 'm'
)

func (f myWordsFilter) statuses() []dictionary.WordStatus {
	switch f {
	case myWordsFilterLearning:
		return []dictionary.WordStatus{dictionary.StatusLearning}
	case myWordsFilterKnown:
		return []dictionary.WordStatus{dictionary.StatusKnown, dictionary.StatusMastered}
	case myWordsFilterMastered:
		return []dictionary.WordStatus{dictionary.StatusKnown, dictionary.StatusMastered}
	default:
		return []dictionary.WordStatus{
			dictionary.StatusLearning,
			dictionary.StatusKnown,
			dictionary.StatusMastered,
		}
	}
}

func (f myWordsFilter) labelKey() string {
	switch f {
	case myWordsFilterLearning:
		return "mywords.filter.learning"
	case myWordsFilterKnown:
		return "mywords.filter.known"
	case myWordsFilterMastered:
		return "mywords.filter.mastered"
	default:
		return "mywords.filter.all"
	}
}

func parseMyWordsFilter(s string) (myWordsFilter, bool) {
	if len(s) != 1 {
		return 0, false
	}
	f := myWordsFilter(s[0])
	switch f {
	case myWordsFilterAll, myWordsFilterLearning, myWordsFilterKnown, myWordsFilterMastered:
		return f, true
	}
	return 0, false
}

// MyWords serves /mywords and the `mw:` callback family. It lists the user's
// vocabulary for the active learning language with All/Learning/Known/
// Mastered filters and a per-word status editor.
type MyWords struct {
	users     *users.Service
	languages users.UserLanguageRepository
	statuses  dictionary.UserWordStatusRepository
	bundle    *goi18n.Bundle
	log       *slog.Logger
	db        *sql.DB
}

func NewMyWords(
	svc *users.Service,
	langs users.UserLanguageRepository,
	statuses dictionary.UserWordStatusRepository,
	db *sql.DB,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *MyWords {
	return &MyWords{
		users:     svc,
		languages: langs,
		statuses:  statuses,
		bundle:    bundle,
		log:       log,
		db:        db,
	}
}

// HandleCommand reacts to /mywords — sends a fresh first page with the All
// filter selected.
func (h *MyWords) HandleCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}
	u, ok := resolveMessageUser(ctx, h.users, msg, h.log, "mywords cmd")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	lang, ok := h.activeLanguage(ctx, u.ID)
	if !ok {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   tgi18n.T(loc, "mywords.no_active", nil),
		})
		return
	}
	text, kb := h.buildList(ctx, u.ID, lang, loc, myWordsFilterAll, 0)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      msg.Chat.ID,
		Text:        text,
		ReplyMarkup: kb,
	})
}

// HandleCallback drives the `mw:` prefix.
func (h *MyWords) HandleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	defer func() {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})
	}()
	u, ok := resolveCallbackUser(ctx, h.users, cq, h.log, "mywords cb")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	chatID, msgID, ok := callbackMessageRef(cq)
	if !ok {
		return
	}

	payload := strings.TrimPrefix(cq.Data, CallbackPrefixMyWords)
	switch {
	case payload == "close":
		b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: msgID})
	case payload == "noop":
	case strings.HasPrefix(payload, "f:"):
		h.handleListPayload(ctx, b, u.ID, loc, chatID, msgID, strings.TrimPrefix(payload, "f:"))
	case strings.HasPrefix(payload, "e:"):
		h.handleEditPayload(ctx, b, u.ID, loc, chatID, msgID, strings.TrimPrefix(payload, "e:"))
	case strings.HasPrefix(payload, "s:"):
		h.handleSetPayload(ctx, b, u.ID, loc, chatID, msgID, strings.TrimPrefix(payload, "s:"))
	default:
		h.log.Warn("mywords cb: unknown payload", "data", cq.Data)
	}
}

func (h *MyWords) handleListPayload(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int, body string) {
	parts := strings.Split(body, ":")
	if len(parts) != 2 {
		h.log.Warn("mywords cb: bad list payload", "body", body)
		return
	}
	f, ok := parseMyWordsFilter(parts[0])
	if !ok {
		h.log.Warn("mywords cb: bad filter", "body", body)
		return
	}
	page, err := strconv.Atoi(parts[1])
	if err != nil {
		h.log.Warn("mywords cb: bad page", "body", body, "err", err)
		return
	}
	lang, ok := h.activeLanguage(ctx, userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "mywords.no_active", nil), nil)
		return
	}
	text, kb := h.buildList(ctx, userID, lang, loc, f, page)
	h.editTo(ctx, b, chatID, msgID, text, kb)
}

func (h *MyWords) handleEditPayload(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int, body string) {
	parts := strings.Split(body, ":")
	if len(parts) != 3 {
		h.log.Warn("mywords cb: bad edit payload", "body", body)
		return
	}
	f, ok := parseMyWordsFilter(parts[0])
	if !ok {
		h.log.Warn("mywords cb: bad filter", "body", body)
		return
	}
	page, err := strconv.Atoi(parts[1])
	if err != nil {
		h.log.Warn("mywords cb: bad page", "body", body, "err", err)
		return
	}
	wordID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		h.log.Warn("mywords cb: bad word id", "body", body, "err", err)
		return
	}
	current, err := h.statuses.Get(ctx, h.db, userID, wordID)
	if err != nil && !errors.Is(err, dictionary.ErrNotFound) {
		h.log.Error("mywords cb: status get", "err", err)
		return
	}
	lang, ok := h.activeLanguage(ctx, userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "mywords.no_active", nil), nil)
		return
	}
	// Pull the lemma directly from the page query so we don't fan out a per-row
	// lookup. The user just clicked one of the visible rows, so it is on the
	// page we'd render anyway.
	views, err := h.statuses.PageUserWords(ctx, h.db, userID, lang, f.statuses(), myWordsPageSize, page*myWordsPageSize)
	if err != nil {
		h.log.Error("mywords cb: page", "err", err)
		return
	}
	var entry *dictionary.UserWordEntry
	for i := range views {
		if views[i].DictionaryWordID == wordID {
			entry = &views[i]
			break
		}
	}
	if entry == nil {
		// The word may have been moved off this page by a concurrent change
		// (e.g. status flipped to skipped). Fall back to the list.
		text, kb := h.buildList(ctx, userID, lang, loc, f, page)
		h.editTo(ctx, b, chatID, msgID, text, kb)
		return
	}
	currentStatus := dictionary.WordStatus("")
	if current != nil {
		currentStatus = current.Status
	}
	text := renderEditScreen(loc, *entry, currentStatus)
	h.editTo(ctx, b, chatID, msgID, text, editKeyboard(loc, f, page, wordID, currentStatus))
}

func (h *MyWords) handleSetPayload(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int, body string) {
	parts := strings.Split(body, ":")
	if len(parts) != 4 {
		h.log.Warn("mywords cb: bad set payload", "body", body)
		return
	}
	f, ok := parseMyWordsFilter(parts[0])
	if !ok {
		h.log.Warn("mywords cb: bad filter", "body", body)
		return
	}
	page, err := strconv.Atoi(parts[1])
	if err != nil {
		h.log.Warn("mywords cb: bad page", "body", body, "err", err)
		return
	}
	wordID, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		h.log.Warn("mywords cb: bad word id", "body", body, "err", err)
		return
	}
	status := dictionary.WordStatus(parts[3])
	switch status {
	case dictionary.StatusLearning, dictionary.StatusKnown,
		dictionary.StatusMastered, dictionary.StatusSkipped:
	default:
		h.log.Warn("mywords cb: bad status", "body", body)
		return
	}
	if err := h.statuses.Upsert(ctx, h.db, dictionary.UserWordStatus{
		UserID:           userID,
		DictionaryWordID: wordID,
		Status:           status,
	}); err != nil {
		h.log.Error("mywords cb: upsert", "err", err)
		return
	}
	lang, ok := h.activeLanguage(ctx, userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "mywords.no_active", nil), nil)
		return
	}
	text, kb := h.buildList(ctx, userID, lang, loc, f, page)
	h.editTo(ctx, b, chatID, msgID, text, kb)
}

func (h *MyWords) buildList(ctx context.Context, userID int64, lang string, loc *goi18n.Localizer, f myWordsFilter, page int) (string, *models.InlineKeyboardMarkup) {
	statuses := f.statuses()
	total, err := h.statuses.CountUserWords(ctx, h.db, userID, lang, statuses)
	if err != nil {
		h.log.Error("mywords: count", "err", err)
		return tgi18n.T(loc, "error.generic", nil), backToCloseKeyboard(loc)
	}
	if total == 0 {
		text := tgi18n.T(loc, "mywords.empty", map[string]string{
			"Filter":   tgi18n.T(loc, f.labelKey(), nil),
			"Language": strings.ToUpper(lang),
		})
		// If the entire vocabulary is empty (not just the current filter),
		// drop the filter row — every filter would be empty, so the buttons
		// are noise.
		if f == myWordsFilterAll {
			return text, backToCloseKeyboard(loc)
		}
		grandTotal, err := h.statuses.CountUserWords(ctx, h.db, userID, lang, myWordsFilterAll.statuses())
		if err != nil {
			h.log.Error("mywords: count all", "err", err)
			return tgi18n.T(loc, "error.generic", nil), backToCloseKeyboard(loc)
		}
		if grandTotal == 0 {
			return text, backToCloseKeyboard(loc)
		}
		return text, filterRowAndCloseKeyboard(loc, f)
	}
	totalPages := (total + myWordsPageSize - 1) / myWordsPageSize
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}
	rows, err := h.statuses.PageUserWords(ctx, h.db, userID, lang, statuses, myWordsPageSize, page*myWordsPageSize)
	if err != nil {
		h.log.Error("mywords: page", "err", err)
		return tgi18n.T(loc, "error.generic", nil), backToCloseKeyboard(loc)
	}
	header := tgi18n.T(loc, "mywords.header", map[string]any{
		"Page":     page + 1,
		"Total":    totalPages,
		"Filter":   tgi18n.T(loc, f.labelKey(), nil),
		"Language": strings.ToUpper(lang),
	})
	return header, listKeyboard(loc, f, page, totalPages, rows)
}

func (h *MyWords) editTo(ctx context.Context, b *bot.Bot, chatID any, msgID int, text string, kb *models.InlineKeyboardMarkup) {
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        text,
		ReplyMarkup: kb,
	}); err != nil {
		h.log.Debug("mywords: edit", "err", err)
	}
}

func (h *MyWords) activeLanguage(ctx context.Context, userID int64) (string, bool) {
	active, err := h.languages.Active(ctx, userID)
	if err != nil || active == nil {
		return "", false
	}
	return active.LanguageCode, true
}

func renderEditScreen(loc *goi18n.Localizer, entry dictionary.UserWordEntry, current dictionary.WordStatus) string {
	var sb strings.Builder
	sb.WriteString(entry.Lemma)
	if entry.POS != "" {
		fmt.Fprintf(&sb, " (%s)", entry.POS)
	}
	sb.WriteString("\n")
	if current != "" {
		sb.WriteString(tgi18n.T(loc, "mywords.edit.current", map[string]string{
			"Status": tgi18n.T(loc, statusLabelKey(current), nil),
		}))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(tgi18n.T(loc, "mywords.edit.prompt", nil))
	return sb.String()
}

func listKeyboard(loc *goi18n.Localizer, f myWordsFilter, page, totalPages int, rows []dictionary.UserWordEntry) *models.InlineKeyboardMarkup {
	out := make([][]models.InlineKeyboardButton, 0, len(rows)+3)
	out = append(out, filterRow(loc, f))
	for _, e := range rows {
		out = append(out, []models.InlineKeyboardButton{{
			Text:         myWordsRowLabel(loc, e),
			CallbackData: fmt.Sprintf("%se:%c:%d:%d", CallbackPrefixMyWords, f, page, e.DictionaryWordID),
		}})
	}
	out = append(out, paginationRow(loc, f, page, totalPages))
	out = append(out, []models.InlineKeyboardButton{closeMyWordsButton(loc)})
	return &models.InlineKeyboardMarkup{InlineKeyboard: out}
}

func filterRowAndCloseKeyboard(loc *goi18n.Localizer, f myWordsFilter) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		filterRow(loc, f),
		{closeMyWordsButton(loc)},
	}}
}

func backToCloseKeyboard(loc *goi18n.Localizer) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{closeMyWordsButton(loc)},
	}}
}

func filterRow(loc *goi18n.Localizer, current myWordsFilter) []models.InlineKeyboardButton {
	all := []myWordsFilter{myWordsFilterAll, myWordsFilterLearning, myWordsFilterKnown}
	row := make([]models.InlineKeyboardButton, 0, len(all))
	for _, f := range all {
		label := tgi18n.T(loc, f.labelKey(), nil)
		if f == current {
			label = "✓ " + label
		}
		row = append(row, models.InlineKeyboardButton{
			Text:         label,
			CallbackData: fmt.Sprintf("%sf:%c:0", CallbackPrefixMyWords, f),
		})
	}
	return row
}

func paginationRow(loc *goi18n.Localizer, f myWordsFilter, page, totalPages int) []models.InlineKeyboardButton {
	prev := models.InlineKeyboardButton{Text: tgi18n.T(loc, "mywords.btn.prev", nil)}
	next := models.InlineKeyboardButton{Text: tgi18n.T(loc, "mywords.btn.next", nil)}
	if page > 0 {
		prev.CallbackData = fmt.Sprintf("%sf:%c:%d", CallbackPrefixMyWords, f, page-1)
	} else {
		prev.CallbackData = CallbackPrefixMyWords + "noop"
	}
	if page < totalPages-1 {
		next.CallbackData = fmt.Sprintf("%sf:%c:%d", CallbackPrefixMyWords, f, page+1)
	} else {
		next.CallbackData = CallbackPrefixMyWords + "noop"
	}
	return []models.InlineKeyboardButton{prev, next}
}

func editKeyboard(loc *goi18n.Localizer, f myWordsFilter, page int, wordID int64, current dictionary.WordStatus) *models.InlineKeyboardMarkup {
	options := []dictionary.WordStatus{
		dictionary.StatusLearning,
		dictionary.StatusKnown,
		dictionary.StatusMastered,
		dictionary.StatusSkipped,
	}
	rows := make([][]models.InlineKeyboardButton, 0, len(options)+1)
	for _, s := range options {
		label := tgi18n.T(loc, statusLabelKey(s), nil)
		if s == current {
			label = "✓ " + label
		}
		rows = append(rows, []models.InlineKeyboardButton{{
			Text:         label,
			CallbackData: fmt.Sprintf("%ss:%c:%d:%d:%s", CallbackPrefixMyWords, f, page, wordID, s),
		}})
	}
	rows = append(rows, []models.InlineKeyboardButton{{
		Text:         tgi18n.T(loc, "mywords.btn.back", nil),
		CallbackData: fmt.Sprintf("%sf:%c:%d", CallbackPrefixMyWords, f, page),
	}})
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func myWordsRowLabel(loc *goi18n.Localizer, e dictionary.UserWordEntry) string {
	icon := statusIcon(e.Status)
	if icon == "" {
		return e.Lemma
	}
	return icon + " " + e.Lemma
}

func statusIcon(s dictionary.WordStatus) string {
	switch s {
	case dictionary.StatusLearning:
		return "📖"
	case dictionary.StatusKnown:
		return "✓"
	case dictionary.StatusMastered:
		return "🎓"
	case dictionary.StatusSkipped:
		return "✗"
	}
	return ""
}

func statusLabelKey(s dictionary.WordStatus) string {
	switch s {
	case dictionary.StatusLearning:
		return "mywords.status.learning"
	case dictionary.StatusKnown:
		return "mywords.status.known"
	case dictionary.StatusMastered:
		return "mywords.status.mastered"
	case dictionary.StatusSkipped:
		return "mywords.status.skipped"
	}
	return "mywords.status.learning"
}

func closeMyWordsButton(loc *goi18n.Localizer) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{
		Text:         tgi18n.T(loc, "mywords.btn.close", nil),
		CallbackData: CallbackPrefixMyWords + "close",
	}
}
