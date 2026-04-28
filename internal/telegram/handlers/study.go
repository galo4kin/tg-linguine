package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nikita/tg-linguine/internal/dictionary"
	tgi18n "github.com/nikita/tg-linguine/internal/i18n"
	"github.com/nikita/tg-linguine/internal/session"
	"github.com/nikita/tg-linguine/internal/users"
)

// CallbackPrefixStudy still drives the `/study` flow even after the
// flashcard mode was replaced with a multiple-choice quiz — the prefix
// string stays the same so /article cards' "show all words → study" hand-
// off keeps working without churn. Payloads:
//
//	study:start                      — start a fresh round (also from /article cards)
//	study:ans:<word_id>:<option_idx> — user picked option N for word
//	study:next                       — advance from feedback to the next card
//	study:end                        — finish the round early
//	study:again                      — start another round from the summary
//	study:close                      — dismiss the summary message
const CallbackPrefixStudy = "study:"

// Study runs quiz rounds (multiple choice) backed by the in-memory
// session.Quiz FSM and the dictionary status repo. The struct keeps the
// historical "Study" name because it is wired into bot.go and external
// callers under /study; internally everything is quiz-shaped.
type Study struct {
	users     *users.Service
	languages users.UserLanguageRepository
	statuses  dictionary.UserWordStatusRepository
	fsm       *session.Quiz
	bundle    *goi18n.Bundle
	log       *slog.Logger
	db        *sql.DB

	rngMu sync.Mutex
	rng   *rand.Rand
}

func NewStudy(
	svc *users.Service,
	langs users.UserLanguageRepository,
	statuses dictionary.UserWordStatusRepository,
	fsm *session.Quiz,
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
		rng:       rand.New(rand.NewSource(rand.Int63())),
	}
}

// HandleCommand reacts to /study — assembles a fresh deck and sends the
// first question.
func (h *Study) HandleCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}
	u, ok := resolveMessageUser(ctx, h.users, msg, h.log, "quiz cmd")
	if !ok {
		return
	}
	loc := tgi18n.For(h.bundle, u.InterfaceLanguage)
	deck, ok := h.buildDeck(ctx, u.ID)
	if !ok {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: msg.Chat.ID,
			Text:   tgi18n.T(loc, "quiz.empty", nil),
		})
		return
	}
	h.fsm.Start(u.ID, deck)
	snap, _ := h.fsm.Snapshot(u.ID)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      msg.Chat.ID,
		Text:        renderQuizQuestion(loc, snap),
		ReplyMarkup: quizQuestionKeyboard(loc, snap),
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
	u, ok := resolveCallbackUser(ctx, h.users, cq, h.log, "quiz cb")
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
	case payload == "start" || payload == "again":
		h.startRound(ctx, b, u.ID, loc, chatID, msgID, payload == "again")
	case payload == "end":
		h.endAndSummarize(ctx, b, u.ID, loc, chatID, msgID)
	case payload == "next":
		h.renderState(ctx, b, u.ID, loc, chatID, msgID)
	case payload == "skip":
		h.handleSkip(ctx, b, u.ID, loc, chatID, msgID)
	case strings.HasPrefix(payload, "del:"):
		h.handleDelete(ctx, b, u.ID, loc, chatID, msgID, payload)
	case strings.HasPrefix(payload, "ans:"):
		h.handleAnswer(ctx, b, u.ID, loc, chatID, msgID, payload)
	default:
		h.log.Warn("quiz cb: unknown payload", "data", cq.Data)
	}
}

func (h *Study) startRound(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int, edit bool) {
	deck, ok := h.buildDeck(ctx, userID)
	if !ok {
		text := tgi18n.T(loc, "quiz.empty", nil)
		if edit {
			h.editTo(ctx, b, chatID, msgID, text, nil)
		} else {
			b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})
		}
		return
	}
	h.fsm.Start(userID, deck)
	snap, _ := h.fsm.Snapshot(userID)
	if edit {
		h.editTo(ctx, b, chatID, msgID, renderQuizQuestion(loc, snap), quizQuestionKeyboard(loc, snap))
	} else {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        renderQuizQuestion(loc, snap),
			ReplyMarkup: quizQuestionKeyboard(loc, snap),
		})
	}
}

func (h *Study) handleAnswer(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int, payload string) {
	parts := strings.SplitN(strings.TrimPrefix(payload, "ans:"), ":", 2)
	if len(parts) != 2 {
		h.log.Warn("quiz cb: bad answer payload", "payload", payload)
		return
	}
	wordID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		h.log.Warn("quiz cb: bad word id", "payload", payload, "err", err)
		return
	}
	optIdx, err := strconv.Atoi(parts[1])
	if err != nil {
		h.log.Warn("quiz cb: bad option idx", "payload", payload, "err", err)
		return
	}
	snap, ok := h.fsm.Snapshot(userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "quiz.expired", nil), quizCloseKeyboard(loc))
		return
	}
	if snap.Done() || snap.Current().DictionaryWordID != wordID {
		// Stale button (double-click after a new round started, etc.).
		h.renderState(ctx, b, userID, loc, chatID, msgID)
		return
	}
	card := snap.Current()
	correct := optIdx == card.CorrectIndex

	var mastered bool
	if correct {
		_, m, err := h.statuses.RecordCorrect(ctx, h.db, userID, wordID, session.QuizMasteryThreshold)
		if err != nil && !errors.Is(err, dictionary.ErrNotFound) {
			h.log.Error("quiz cb: record correct", "err", err)
			return
		}
		mastered = m
	} else {
		if err := h.statuses.RecordWrong(ctx, h.db, userID, wordID); err != nil && !errors.Is(err, dictionary.ErrNotFound) {
			h.log.Error("quiz cb: record wrong", "err", err)
			return
		}
	}
	if _, ok := h.fsm.RecordAnswer(userID, correct, mastered); !ok {
		return
	}
	// Show feedback for this card with a "Next" button before advancing the
	// rendered card. The FSM cursor has already advanced; we render the
	// feedback off `card` (the answered one) and `optIdx` (the user's choice).
	h.editToHTML(ctx, b, chatID, msgID, renderQuizFeedback(loc, snap, card, optIdx, correct, mastered), quizFeedbackKeyboard(loc))
}

func (h *Study) handleSkip(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int) {
	snap, ok := h.fsm.Snapshot(userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "quiz.expired", nil), quizCloseKeyboard(loc))
		return
	}
	if snap.Done() {
		h.renderState(ctx, b, userID, loc, chatID, msgID)
		return
	}
	card := snap.Current()
	if _, ok := h.fsm.RecordSkip(userID); !ok {
		return
	}
	h.editToHTML(ctx, b, chatID, msgID, renderQuizSkipFeedback(loc, snap, card), quizFeedbackKeyboard(loc))
}

func (h *Study) handleDelete(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int, payload string) {
	wordIDStr := strings.TrimPrefix(payload, "del:")
	wordID, err := strconv.ParseInt(wordIDStr, 10, 64)
	if err != nil {
		h.log.Warn("quiz cb: bad delete payload", "payload", payload)
		return
	}
	snap, ok := h.fsm.Snapshot(userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "quiz.expired", nil), quizCloseKeyboard(loc))
		return
	}
	if snap.Done() {
		h.renderState(ctx, b, userID, loc, chatID, msgID)
		return
	}
	if snap.Current().DictionaryWordID != wordID {
		h.renderState(ctx, b, userID, loc, chatID, msgID)
		return
	}
	card := snap.Current()
	if err := h.statuses.DeleteWordStatus(ctx, h.db, userID, wordID); err != nil {
		h.log.Error("quiz cb: delete word status", "err", err)
		return
	}
	if _, ok := h.fsm.RecordSkip(userID); !ok {
		return
	}
	h.editToHTML(ctx, b, chatID, msgID, renderQuizDeleteFeedback(loc, snap, card), quizFeedbackKeyboard(loc))
}

func (h *Study) renderState(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int) {
	snap, ok := h.fsm.Snapshot(userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "quiz.expired", nil), quizCloseKeyboard(loc))
		return
	}
	if snap.Done() {
		final, _ := h.fsm.End(userID)
		h.editTo(ctx, b, chatID, msgID, renderQuizSummary(loc, final), quizSummaryKeyboard(loc))
		return
	}
	h.editTo(ctx, b, chatID, msgID, renderQuizQuestion(loc, snap), quizQuestionKeyboard(loc, snap))
}

func (h *Study) endAndSummarize(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int) {
	final, ok := h.fsm.End(userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "quiz.expired", nil), quizCloseKeyboard(loc))
		return
	}
	h.editTo(ctx, b, chatID, msgID, renderQuizSummary(loc, final), quizSummaryKeyboard(loc))
}

func (h *Study) editTo(ctx context.Context, b *bot.Bot, chatID any, msgID int, text string, kb *models.InlineKeyboardMarkup) {
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        text,
		ReplyMarkup: kb,
	}); err != nil {
		h.log.Debug("quiz: edit", "err", err)
	}
}

func (h *Study) editToHTML(ctx context.Context, b *bot.Bot, chatID any, msgID int, text string, kb *models.InlineKeyboardMarkup) {
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   msgID,
		Text:        text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	}); err != nil {
		h.log.Debug("quiz: edit html", "err", err)
	}
}

func (h *Study) buildDeck(ctx context.Context, userID int64) ([]session.QuizCard, bool) {
	active, err := h.languages.Active(ctx, userID)
	if err != nil || active == nil {
		return nil, false
	}
	queue, err := h.statuses.LearningQueue(ctx, h.db, userID, active.LanguageCode, session.QuizDeckSize)
	if err != nil {
		h.log.Error("quiz: learning queue", "err", err)
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
		h.log.Error("quiz: sample article_words", "err", err)
		return nil, false
	}
	deck := make([]session.QuizCard, 0, len(queue))
	for _, e := range queue {
		var translation, exampleT, exampleN string
		if s, ok := samples[e.DictionaryWordID]; ok {
			translation = strings.TrimSpace(s.TranslationNative)
			exampleT = s.ExampleTarget
			exampleN = s.ExampleNative
		}
		// Without a translation we can't form either direction's question.
		if translation == "" {
			continue
		}

		direction := h.pickDirection()
		// Step 50: inline-only. Step 51 introduces poll; step 52 lifts the
		// fixed mode and lets the FSM mix both within one round.
		ui := session.QuizUIInline

		var dir dictionary.DistractorDirection
		var correct string
		if direction == session.QuizForeignToNative {
			dir = dictionary.DistractorForeignToNative
			correct = translation
		} else {
			dir = dictionary.DistractorNativeToForeign
			correct = e.Lemma
		}
		// Multi-word answers don't fit the compact 2×2 button layout.
		if strings.ContainsAny(correct, " \t\n") {
			continue
		}
		distractors, err := h.statuses.SampleDistractors(ctx, h.db, userID, active.LanguageCode, e.DictionaryWordID, correct, dir, 3)
		if err != nil {
			h.log.Error("quiz: sample distractors", "err", err)
			continue
		}
		distractors = filterSingleWord(distractors)
		if len(distractors) < 3 {
			// Not enough variety in this user's language yet — skip rather
			// than showing a 2-option "quiz".
			continue
		}
		opts, idx := h.buildOptions(correct, distractors, 4)
		deck = append(deck, session.QuizCard{
			DictionaryWordID:  e.DictionaryWordID,
			Lemma:             e.Lemma,
			POS:               e.POS,
			TranscriptionIPA:  e.TranscriptionIPA,
			TranslationNative: translation,
			ExampleTarget:     exampleT,
			ExampleNative:     exampleN,
			Direction:         direction,
			UIMode:            ui,
			Options:           opts,
			CorrectIndex:      idx,
		})
	}
	return deck, len(deck) > 0
}

func (h *Study) pickDirection() session.QuizDirection {
	h.rngMu.Lock()
	defer h.rngMu.Unlock()
	return session.PickQuizDirection(h.rng)
}

func (h *Study) buildOptions(correct string, distractors []string, want int) ([]string, int) {
	h.rngMu.Lock()
	defer h.rngMu.Unlock()
	return session.BuildQuizOptions(h.rng, correct, distractors, want)
}

func renderQuizQuestion(loc *goi18n.Localizer, snap session.QuizSnapshot) string {
	c := snap.Current()
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n\n", tgi18n.T(loc, "quiz.card.header", map[string]int{
		"Index": snap.Cursor + 1,
		"Total": len(snap.Deck),
	}))
	switch c.Direction {
	case session.QuizForeignToNative:
		sb.WriteString(c.Lemma)
		if c.POS != "" {
			fmt.Fprintf(&sb, " (%s)", c.POS)
		}
		if c.TranscriptionIPA != "" {
			fmt.Fprintf(&sb, " /%s/", c.TranscriptionIPA)
		}
	case session.QuizNativeToForeign:
		sb.WriteString(c.TranslationNative)
	}
	sb.WriteString("\n\n")
	sb.WriteString(tgi18n.T(loc, "quiz.card.prompt", nil))
	return strings.TrimRight(sb.String(), "\n")
}

// renderQuizFeedback returns an HTML-formatted feedback message.
func renderQuizFeedback(loc *goi18n.Localizer, snap session.QuizSnapshot, card session.QuizCard, picked int, correct, mastered bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n\n", tgi18n.T(loc, "quiz.card.header", map[string]int{
		"Index": snap.Cursor + 1,
		"Total": len(snap.Deck),
	}))
	sb.WriteString("<code>")
	switch card.Direction {
	case session.QuizForeignToNative:
		sb.WriteString(htmlEscape(card.Lemma))
		if card.POS != "" {
			fmt.Fprintf(&sb, " (%s)", htmlEscape(card.POS))
		}
		if card.TranscriptionIPA != "" {
			fmt.Fprintf(&sb, " /%s/", htmlEscape(card.TranscriptionIPA))
		}
	case session.QuizNativeToForeign:
		sb.WriteString(htmlEscape(card.TranslationNative))
	}
	sb.WriteString("</code>")
	sb.WriteString("\n\n")
	if correct {
		sb.WriteString(tgi18n.T(loc, "quiz.feedback.correct", nil))
	} else {
		sb.WriteString(tgi18n.T(loc, "quiz.feedback.wrong", map[string]any{
			"Picked":  htmlEscape(card.Options[picked]),
			"Correct": htmlEscape(card.Options[card.CorrectIndex]),
		}))
	}
	if mastered {
		sb.WriteString("\n")
		sb.WriteString(tgi18n.T(loc, "quiz.feedback.mastered", map[string]any{"Lemma": htmlEscape(card.Lemma)}))
	}
	if card.ExampleTarget != "" || card.ExampleNative != "" {
		sb.WriteString("\n\n<blockquote>")
		if card.ExampleTarget != "" {
			sb.WriteString(htmlEscape(card.ExampleTarget))
			if card.ExampleNative != "" {
				sb.WriteString("\n")
			}
		}
		if card.ExampleNative != "" {
			sb.WriteString(htmlEscape(card.ExampleNative))
		}
		sb.WriteString("</blockquote>")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func renderQuizSummary(loc *goi18n.Localizer, snap session.QuizSnapshot) string {
	var sb strings.Builder
	sb.WriteString(tgi18n.T(loc, "quiz.summary.header", map[string]int{
		"Correct": snap.Correct,
		"Wrong":   snap.Wrong,
		"Total":   len(snap.Deck),
	}))
	sb.WriteString("\n")
	if len(snap.Mastered) == 0 {
		sb.WriteString("\n")
		sb.WriteString(tgi18n.T(loc, "quiz.summary.no_mastered", nil))
		return sb.String()
	}
	sb.WriteString("\n")
	sb.WriteString(tgi18n.T(loc, "quiz.summary.mastered_header", map[string]int{
		"Count": len(snap.Mastered),
	}))
	sb.WriteString("\n")
	for _, lemma := range snap.Mastered {
		fmt.Fprintf(&sb, "• %s\n", lemma)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func filterSingleWord(items []string) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		if !strings.ContainsAny(s, " \t\n") {
			out = append(out, s)
		}
	}
	return out
}

func renderQuizSkipFeedback(loc *goi18n.Localizer, snap session.QuizSnapshot, card session.QuizCard) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n\n", tgi18n.T(loc, "quiz.card.header", map[string]int{
		"Index": snap.Cursor + 1,
		"Total": len(snap.Deck),
	}))
	sb.WriteString("<code>")
	switch card.Direction {
	case session.QuizForeignToNative:
		sb.WriteString(htmlEscape(card.Lemma))
		if card.POS != "" {
			fmt.Fprintf(&sb, " (%s)", htmlEscape(card.POS))
		}
		if card.TranscriptionIPA != "" {
			fmt.Fprintf(&sb, " /%s/", htmlEscape(card.TranscriptionIPA))
		}
	case session.QuizNativeToForeign:
		sb.WriteString(htmlEscape(card.TranslationNative))
	}
	sb.WriteString("</code>")
	sb.WriteString("\n\n")
	sb.WriteString(tgi18n.T(loc, "quiz.feedback.skipped", map[string]any{
		"Correct": htmlEscape(card.Options[card.CorrectIndex]),
	}))
	if card.ExampleTarget != "" || card.ExampleNative != "" {
		sb.WriteString("\n\n<blockquote>")
		if card.ExampleTarget != "" {
			sb.WriteString(htmlEscape(card.ExampleTarget))
			if card.ExampleNative != "" {
				sb.WriteString("\n")
			}
		}
		if card.ExampleNative != "" {
			sb.WriteString(htmlEscape(card.ExampleNative))
		}
		sb.WriteString("</blockquote>")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func renderQuizDeleteFeedback(loc *goi18n.Localizer, snap session.QuizSnapshot, card session.QuizCard) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\n\n", tgi18n.T(loc, "quiz.card.header", map[string]int{
		"Index": snap.Cursor + 1,
		"Total": len(snap.Deck),
	}))
	sb.WriteString("<code>")
	switch card.Direction {
	case session.QuizForeignToNative:
		sb.WriteString(htmlEscape(card.Lemma))
		if card.POS != "" {
			fmt.Fprintf(&sb, " (%s)", htmlEscape(card.POS))
		}
	case session.QuizNativeToForeign:
		sb.WriteString(htmlEscape(card.TranslationNative))
	}
	sb.WriteString("</code>")
	sb.WriteString("\n\n")
	sb.WriteString(tgi18n.T(loc, "quiz.feedback.deleted", nil))
	return sb.String()
}

func quizQuestionKeyboard(loc *goi18n.Localizer, snap session.QuizSnapshot) *models.InlineKeyboardMarkup {
	c := snap.Current()
	rows := make([][]models.InlineKeyboardButton, 0, 3) // 2 answer rows + 1 control row
	for i := 0; i < len(c.Options); i += 2 {
		row := []models.InlineKeyboardButton{{
			Text:         c.Options[i],
			CallbackData: fmt.Sprintf("%sans:%d:%d", CallbackPrefixStudy, c.DictionaryWordID, i),
		}}
		if i+1 < len(c.Options) {
			row = append(row, models.InlineKeyboardButton{
				Text:         c.Options[i+1],
				CallbackData: fmt.Sprintf("%sans:%d:%d", CallbackPrefixStudy, c.DictionaryWordID, i+1),
			})
		}
		rows = append(rows, row)
	}
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: tgi18n.T(loc, "quiz.btn.skip", nil), CallbackData: CallbackPrefixStudy + "skip"},
		{Text: tgi18n.T(loc, "quiz.btn.del", nil), CallbackData: fmt.Sprintf("%sdel:%d", CallbackPrefixStudy, c.DictionaryWordID)},
		{Text: tgi18n.T(loc, "quiz.btn.end", nil), CallbackData: CallbackPrefixStudy + "end"},
	})
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func quizFeedbackKeyboard(loc *goi18n.Localizer) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{{Text: tgi18n.T(loc, "quiz.btn.next", nil), CallbackData: CallbackPrefixStudy + "next"}},
	}}
}

func quizSummaryKeyboard(loc *goi18n.Localizer) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{{Text: tgi18n.T(loc, "quiz.btn.again", nil), CallbackData: CallbackPrefixStudy + "again"}},
		{{Text: tgi18n.T(loc, "quiz.btn.close", nil), CallbackData: CallbackPrefixStudy + "close"}},
	}}
}

func quizCloseKeyboard(loc *goi18n.Localizer) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{{Text: tgi18n.T(loc, "quiz.btn.close", nil), CallbackData: CallbackPrefixStudy + "close"}},
	}}
}
