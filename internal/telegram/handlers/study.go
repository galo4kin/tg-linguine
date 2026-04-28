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
	"github.com/nikita/tg-linguine/internal/session"
	"github.com/nikita/tg-linguine/internal/users"
)

const studyDeckSize = 10

// CallbackPrefixStudy drives the `/study` flow. Payloads:
//
//	study:hit:<word_id>  — user remembered the card
//	study:miss:<word_id> — user did not
//	study:end            — finish session early
//	study:close          — dismiss the summary message
const CallbackPrefixStudy = "study:"

// Study runs flashcard sessions backed by the in-memory FSM
// (internal/session.Study) and the dictionary status repo. One active
// session per user.
type Study struct {
	users     *users.Service
	languages users.UserLanguageRepository
	statuses  dictionary.UserWordStatusRepository
	fsm       *session.Study
	bundle    *goi18n.Bundle
	log       *slog.Logger
	db        *sql.DB
}

func NewStudy(
	svc *users.Service,
	langs users.UserLanguageRepository,
	statuses dictionary.UserWordStatusRepository,
	fsm *session.Study,
	db *sql.DB,
	bundle *goi18n.Bundle,
	log *slog.Logger,
) *Study {
	return &Study{
		users:     svc,
		languages: langs,
		statuses:  statuses,
		fsm:       fsm,
		bundle:    bundle,
		log:       log,
		db:        db,
	}
}

// HandleCommand reacts to /study — assembles a fresh deck and sends the
// first card.
func (h *Study) HandleCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}
	u, ok := resolveMessageUser(ctx, h.users, msg, h.log, "study cmd")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	deck, ok := h.buildDeck(ctx, u.ID, loc)
	if !ok {
		// Localized notice was already sent.
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   tgi18n.T(loc, "study.empty", nil),
		})
		return
	}
	h.fsm.Start(u.ID, deck)
	snap, _ := h.fsm.Snapshot(u.ID)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      msg.Chat.ID,
		Text:        renderStudyCard(loc, snap),
		ReplyMarkup: studyCardKeyboard(loc, snap.Current().DictionaryWordID),
	})
}

// HandleCallback drives the `study:` prefix.
func (h *Study) HandleCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	cq := update.CallbackQuery
	if cq == nil {
		return
	}
	defer func() {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: cq.ID})
	}()
	u, ok := resolveCallbackUser(ctx, h.users, cq, h.log, "study cb")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	chatID, msgID, ok := callbackMessageRef(cq)
	if !ok {
		return
	}
	payload := strings.TrimPrefix(cq.Data, CallbackPrefixStudy)
	switch {
	case payload == "close":
		b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: msgID})
	case payload == "end":
		h.endAndSummarize(ctx, b, u.ID, loc, chatID, msgID)
	case strings.HasPrefix(payload, "hit:") || strings.HasPrefix(payload, "miss:"):
		h.handleAnswer(ctx, b, u.ID, loc, chatID, msgID, payload)
	default:
		h.log.Warn("study cb: unknown payload", "data", cq.Data)
	}
}

func (h *Study) handleAnswer(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int, payload string) {
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		h.log.Warn("study cb: bad answer payload", "payload", payload)
		return
	}
	wordID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		h.log.Warn("study cb: bad word id", "payload", payload, "err", err)
		return
	}
	snap, ok := h.fsm.Snapshot(userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "study.expired", nil), studyCloseKeyboard(loc))
		return
	}
	if snap.Done() || snap.Current().DictionaryWordID != wordID {
		// Stale button (e.g. the user double-clicks an old card after a new
		// session started). Re-render whatever the FSM currently has.
		h.renderState(ctx, b, userID, loc, chatID, msgID)
		return
	}

	switch parts[0] {
	case "hit":
		streak, mastered, err := h.statuses.RecordCorrect(ctx, h.db, userID, wordID, session.StudyMasteryThreshold)
		if err != nil {
			if errors.Is(err, dictionary.ErrNotFound) {
				h.log.Warn("study cb: missing status row", "user_id", userID, "word_id", wordID)
			} else {
				h.log.Error("study cb: record correct", "err", err)
				return
			}
		}
		_ = streak
		if _, ok := h.fsm.RecordCorrect(userID, mastered); !ok {
			return
		}
	case "miss":
		if err := h.statuses.RecordWrong(ctx, h.db, userID, wordID); err != nil && !errors.Is(err, dictionary.ErrNotFound) {
			h.log.Error("study cb: record wrong", "err", err)
			return
		}
		if _, ok := h.fsm.RecordWrong(userID); !ok {
			return
		}
	}
	h.renderState(ctx, b, userID, loc, chatID, msgID)
}

func (h *Study) renderState(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int) {
	snap, ok := h.fsm.Snapshot(userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "study.expired", nil), studyCloseKeyboard(loc))
		return
	}
	if snap.Done() {
		final, _ := h.fsm.End(userID)
		h.editTo(ctx, b, chatID, msgID, renderStudySummary(loc, final), studyCloseKeyboard(loc))
		return
	}
	h.editTo(ctx, b, chatID, msgID,
		renderStudyCard(loc, snap),
		studyCardKeyboard(loc, snap.Current().DictionaryWordID),
	)
}

func (h *Study) endAndSummarize(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int) {
	final, ok := h.fsm.End(userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "study.expired", nil), studyCloseKeyboard(loc))
		return
	}
	h.editTo(ctx, b, chatID, msgID, renderStudySummary(loc, final), studyCloseKeyboard(loc))
}

func (h *Study) editTo(ctx context.Context, b *bot.Bot, chatID any, msgID int, text string, kb *models.InlineKeyboardMarkup) {
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        text,
		ReplyMarkup: kb,
	}); err != nil {
		h.log.Debug("study: edit", "err", err)
	}
}

func (h *Study) buildDeck(ctx context.Context, userID int64, loc *goi18n.Localizer) ([]session.StudyCard, bool) {
	active, err := h.languages.Active(ctx, userID)
	if err != nil || active == nil {
		return nil, false
	}
	queue, err := h.statuses.LearningQueue(ctx, h.db, userID, active.LanguageCode, studyDeckSize)
	if err != nil {
		h.log.Error("study: learning queue", "err", err)
		return nil, false
	}
	if len(queue) == 0 {
		return nil, false
	}
	wordIDs := make([]int64, len(queue))
	for i, e := range queue {
		wordIDs[i] = e.DictionaryWordID
	}
	samples, err := h.statuses.SampleArticleWords(ctx, h.db, wordIDs)
	if err != nil {
		h.log.Error("study: sample article_words", "err", err)
		return nil, false
	}
	deck := make([]session.StudyCard, 0, len(queue))
	for _, e := range queue {
		card := session.StudyCard{
			DictionaryWordID: e.DictionaryWordID,
			Lemma:            e.Lemma,
			POS:              e.POS,
			TranscriptionIPA: e.TranscriptionIPA,
			SurfaceForm:      e.Lemma,
		}
		if s, ok := samples[e.DictionaryWordID]; ok {
			if s.SurfaceForm != "" {
				card.SurfaceForm = s.SurfaceForm
			}
			card.TranslationNative = s.TranslationNative
			card.ExampleTarget = s.ExampleTarget
			card.ExampleNative = s.ExampleNative
		}
		deck = append(deck, card)
	}
	return deck, true
}

func renderStudyCard(loc *goi18n.Localizer, snap session.StudySnapshot) string {
	c := snap.Current()
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n\n", tgi18n.T(loc, "study.card.header", map[string]int{
		"Index": snap.Cursor + 1,
		"Total": len(snap.Deck),
	}))
	sb.WriteString(c.SurfaceForm)
	if c.POS != "" {
		fmt.Fprintf(&sb, " (%s)", c.POS)
	}
	if c.TranscriptionIPA != "" {
		fmt.Fprintf(&sb, " /%s/", c.TranscriptionIPA)
	}
	sb.WriteString("\n")
	if c.ExampleTarget != "" {
		fmt.Fprintf(&sb, "\n%s\n", c.ExampleTarget)
	}
	if c.ExampleNative != "" {
		fmt.Fprintf(&sb, "%s\n", c.ExampleNative)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func renderStudySummary(loc *goi18n.Localizer, snap session.StudySnapshot) string {
	var sb strings.Builder
	sb.WriteString(tgi18n.T(loc, "study.summary.header", map[string]int{
		"Correct": snap.Correct,
		"Wrong":   snap.Wrong,
	}))
	sb.WriteString("\n")
	if len(snap.Mastered) == 0 {
		sb.WriteString(tgi18n.T(loc, "study.summary.no_mastered", nil))
		return sb.String()
	}
	sb.WriteString("\n")
	sb.WriteString(tgi18n.T(loc, "study.summary.mastered_header", map[string]int{
		"Count": len(snap.Mastered),
	}))
	sb.WriteString("\n")
	for _, lemma := range snap.Mastered {
		fmt.Fprintf(&sb, "• %s\n", lemma)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func studyCardKeyboard(loc *goi18n.Localizer, wordID int64) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{
			{Text: tgi18n.T(loc, "study.btn.hit", nil), CallbackData: fmt.Sprintf("%shit:%d", CallbackPrefixStudy, wordID)},
			{Text: tgi18n.T(loc, "study.btn.miss", nil), CallbackData: fmt.Sprintf("%smiss:%d", CallbackPrefixStudy, wordID)},
		},
		{{Text: tgi18n.T(loc, "study.btn.end", nil), CallbackData: CallbackPrefixStudy + "end"}},
	}}
}

func studyCloseKeyboard(loc *goi18n.Localizer) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{{Text: tgi18n.T(loc, "study.btn.close", nil), CallbackData: CallbackPrefixStudy + "close"}},
	}}
}
