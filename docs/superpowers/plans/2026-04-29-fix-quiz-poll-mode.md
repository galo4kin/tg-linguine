# Fix Quiz (Poll) Mode UX — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the native Telegram quiz poll mode so it behaves like inline mode — previous messages are deleted on transitions, control buttons appear under polls, and the poll is replaced by a feedback card after answering.

**Architecture:** All changes are in the handler layer (`study.go`) plus a one-line middleware fix in `bot.go`. A new `lastBotMessage` map on the `Study` struct tracks the most recent bot message per user, enabling `DeleteMessage` on any transition. Poll-specific callbacks (skip/delete/end) branch on `card.UIMode` to use delete+send instead of edit-in-place.

**Tech Stack:** Go, go-telegram/bot v1.20.0, existing session.Quiz FSM and session.QuizPolls registry.

**Spec:** `docs/superpowers/specs/2026-04-29-fix-quiz-poll-mode-design.md`

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/telegram/handlers/study.go` | Modify | lastMsg tracking, poll ReplyMarkup, delete-based transitions, poll-aware skip/delete/end |
| `internal/telegram/bot.go` | Modify | PollAnswer branch in touchLastSeenMiddleware |

---

### Task 1: Add last-message tracking infrastructure to Study struct

**Files:**
- Modify: `internal/telegram/handlers/study.go:50-100`

- [ ] **Step 1: Add lastBotMessage type and fields to Study struct**

After `roundXP map[int64]int` (line 70), add:

```go
type lastBotMessage struct {
	ChatID    int64
	MessageID int
}
```

Add fields to `Study` struct after the `roundXP` block:

```go
	lastMsgMu sync.Mutex
	lastMsg   map[int64]lastBotMessage
```

- [ ] **Step 2: Initialize lastMsg in NewStudy**

In `NewStudy` (line 85-99), add `lastMsg: make(map[int64]lastBotMessage)` to the struct literal, next to `roundXP: make(map[int64]int)`.

- [ ] **Step 3: Add setLastMsg, takeLastMsg, clearLastMsg methods**

Add after `resetRoundXP` (after line 293):

```go
func (h *Study) setLastMsg(userID int64, chatID int64, msgID int) {
	h.lastMsgMu.Lock()
	defer h.lastMsgMu.Unlock()
	h.lastMsg[userID] = lastBotMessage{ChatID: chatID, MessageID: msgID}
}

func (h *Study) takeLastMsg(userID int64) (lastBotMessage, bool) {
	h.lastMsgMu.Lock()
	defer h.lastMsgMu.Unlock()
	lm, ok := h.lastMsg[userID]
	if ok {
		delete(h.lastMsg, userID)
	}
	return lm, ok
}

func (h *Study) clearLastMsg(userID int64) {
	h.lastMsgMu.Lock()
	defer h.lastMsgMu.Unlock()
	delete(h.lastMsg, userID)
}
```

- [ ] **Step 4: Add deleteLastMessage helper**

Add after the three methods above:

```go
func (h *Study) deleteLastMessage(ctx context.Context, b *bot.Bot, userID int64) {
	lm, ok := h.takeLastMsg(userID)
	if !ok {
		return
	}
	if _, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    lm.ChatID,
		MessageID: lm.MessageID,
	}); err != nil {
		h.log.Debug("quiz: delete last msg", "err", err)
	}
}
```

- [ ] **Step 5: Clear lastMsg on round start**

In `HandleCommand` (line 122-124), after `h.resetRoundXP(u.ID)` add:

```go
h.clearLastMsg(u.ID)
```

In `startRound` (line 181-183), after `h.resetRoundXP(userID)` add:

```go
h.clearLastMsg(userID)
```

- [ ] **Step 6: Build and verify**

Run: `make build`
Expected: clean build, no errors.

- [ ] **Step 7: Commit**

```bash
git add internal/telegram/handlers/study.go
git commit -m "step 56: add lastBotMessage tracking to Study struct"
```

---

### Task 2: Add control buttons under polls

**Files:**
- Modify: `internal/telegram/handlers/study.go:723-820`

- [ ] **Step 1: Add quizPollControlKeyboard function**

Add after `quizCloseKeyboard` (after line 764):

```go
func quizPollControlKeyboard(loc *goi18n.Localizer, card session.QuizCard) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
		{
			{Text: tgi18n.T(loc, "quiz.btn.skip", nil), CallbackData: CallbackPrefixStudy + "skip"},
			{Text: tgi18n.T(loc, "quiz.btn.del", nil), CallbackData: fmt.Sprintf("%sdel:%d", CallbackPrefixStudy, card.DictionaryWordID)},
			{Text: tgi18n.T(loc, "quiz.btn.end", nil), CallbackData: CallbackPrefixStudy + "end"},
		},
	}}
}
```

- [ ] **Step 2: Attach ReplyMarkup to SendPoll in sendCurrentCard**

In `sendCurrentCard` (line 802-809), add `ReplyMarkup` to `SendPollParams`. The full `SendPoll` call becomes:

```go
	msg, err := b.SendPoll(ctx, &bot.SendPollParams{
		ChatID:          chatID,
		Question:        question,
		Options:         options,
		Type:            "quiz",
		CorrectOptionID: card.CorrectIndex,
		IsAnonymous:     &isAnonymous,
		ReplyMarkup:     quizPollControlKeyboard(loc, card),
	})
```

Note: `sendCurrentCard` needs the `loc` parameter — it already has it in its signature.

- [ ] **Step 3: Build and verify**

Run: `make build`
Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add internal/telegram/handlers/study.go
git commit -m "step 56: add skip/delete/end buttons to quiz polls"
```

---

### Task 3: Track last message in sendCurrentCard

**Files:**
- Modify: `internal/telegram/handlers/study.go:778-820`

- [ ] **Step 1: Track inline message in sendCurrentCard**

In `sendCurrentCard`, the inline branch (line 784-790) currently discards the return value. Change to capture and track:

```go
	if card.UIMode != session.QuizUIPoll {
		sent, _ := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        renderQuizQuestion(loc, snap),
			ReplyMarkup: quizQuestionKeyboard(loc, snap),
		})
		if sent != nil {
			h.setLastMsg(userID, chatID, sent.ID)
		}
		return
	}
```

- [ ] **Step 2: Track poll message in sendCurrentCard**

In the poll branch, after the `h.polls.Add(...)` call (line 814-819), add:

```go
	h.setLastMsg(userID, chatID, msg.ID)
```

- [ ] **Step 3: Build and verify**

Run: `make build`
Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add internal/telegram/handlers/study.go
git commit -m "step 56: track last bot message for delete-on-transition"
```

---

### Task 4: Delete poll after answer in HandlePollAnswer

**Files:**
- Modify: `internal/telegram/handlers/study.go:826-897`

- [ ] **Step 1: Delete poll message before sending feedback**

In `HandlePollAnswer`, after the `prev := session.QuizSnapshot{...}` line (line 888) and before the `b.SendMessage` call (line 889), add the delete call:

```go
	if _, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    entry.ChatID,
		MessageID: entry.MessageID,
	}); err != nil {
		h.log.Debug("quiz poll: delete poll msg", "err", err)
	}
```

- [ ] **Step 2: Track the feedback message**

Change the `b.SendMessage` call (line 889-896) to capture the result and track it:

```go
	fbMsg, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      entry.ChatID,
		Text:        renderQuizFeedback(loc, prev, card, picked, correct, mastered, progressInfo),
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: quizFeedbackKeyboard(loc),
	})
	if err != nil {
		h.log.Error("quiz poll: send feedback", "err", err)
	}
	if fbMsg != nil {
		h.setLastMsg(entry.UserID, entry.ChatID, fbMsg.ID)
	}
```

- [ ] **Step 3: Build and verify**

Run: `make build`
Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add internal/telegram/handlers/study.go
git commit -m "step 56: delete poll and show feedback card after answer"
```

---

### Task 5: Delete previous message on transitions in renderState

**Files:**
- Modify: `internal/telegram/handlers/study.go:343-367`

- [ ] **Step 1: Replace clearKeyboard with deleteMessage in renderState poll branch**

In `renderState` (line 361-364), replace:

```go
	if snap.Current().UIMode == session.QuizUIPoll {
		h.clearKeyboard(ctx, b, chatID, msgID)
		h.sendCurrentCard(ctx, b, userID, loc, chatIDInt64(chatID))
		return
	}
```

with:

```go
	if snap.Current().UIMode == session.QuizUIPoll {
		b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: msgID})
		h.sendCurrentCard(ctx, b, userID, loc, chatIDInt64(chatID))
		return
	}
```

- [ ] **Step 2: Build and verify**

Run: `make build`
Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add internal/telegram/handlers/study.go
git commit -m "step 56: delete feedback message before showing next poll"
```

---

### Task 6: Poll-aware handleSkip

**Files:**
- Modify: `internal/telegram/handlers/study.go:295-310`

- [ ] **Step 1: Add poll branch to handleSkip**

Replace the current `handleSkip` (lines 295-310):

```go
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
```

with:

```go
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
	isPoll := card.UIMode == session.QuizUIPoll
	if _, ok := h.fsm.RecordSkip(userID); !ok {
		return
	}
	if isPoll {
		h.polls.DropForUser(userID)
		b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: msgID})
		h.sendHTMLAndTrack(ctx, b, userID, chatIDInt64(chatID), renderQuizSkipFeedback(loc, snap, card), quizFeedbackKeyboard(loc))
		return
	}
	h.editToHTML(ctx, b, chatID, msgID, renderQuizSkipFeedback(loc, snap, card), quizFeedbackKeyboard(loc))
}
```

- [ ] **Step 2: Add sendHTMLAndTrack helper**

This helper is used by all poll-aware handlers to delete+send+track. Add it after `deleteLastMessage`:

```go
func (h *Study) sendHTMLAndTrack(ctx context.Context, b *bot.Bot, userID int64, chatID int64, text string, kb *models.InlineKeyboardMarkup) {
	sent, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	})
	if err != nil {
		h.log.Error("quiz: send html", "err", err)
		return
	}
	if sent != nil {
		h.setLastMsg(userID, chatID, sent.ID)
	}
}
```

- [ ] **Step 3: Build and verify**

Run: `make build`
Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add internal/telegram/handlers/study.go
git commit -m "step 56: poll-aware handleSkip with delete+send"
```

---

### Task 7: Poll-aware handleDelete

**Files:**
- Modify: `internal/telegram/handlers/study.go:312-341`

- [ ] **Step 1: Add poll branch to handleDelete**

Replace the current `handleDelete` (lines 312-341):

```go
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
```

with:

```go
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
	isPoll := card.UIMode == session.QuizUIPoll
	if err := h.statuses.DeleteWordStatus(ctx, h.db, userID, wordID); err != nil {
		h.log.Error("quiz cb: delete word status", "err", err)
		return
	}
	if _, ok := h.fsm.RecordSkip(userID); !ok {
		return
	}
	if isPoll {
		h.polls.DropForUser(userID)
		b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: msgID})
		h.sendHTMLAndTrack(ctx, b, userID, chatIDInt64(chatID), renderQuizDeleteFeedback(loc, snap, card), quizFeedbackKeyboard(loc))
		return
	}
	h.editToHTML(ctx, b, chatID, msgID, renderQuizDeleteFeedback(loc, snap, card), quizFeedbackKeyboard(loc))
}
```

- [ ] **Step 2: Build and verify**

Run: `make build`
Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add internal/telegram/handlers/study.go
git commit -m "step 56: poll-aware handleDelete with delete+send"
```

---

### Task 8: Poll-aware endAndSummarize

**Files:**
- Modify: `internal/telegram/handlers/study.go:369-379`

- [ ] **Step 1: Add poll branch to endAndSummarize**

The tricky part: when the user clicks "end" on a poll's buttons, `editTo` will fail silently because you can't edit a poll's text. We need to detect this case. The FSM hasn't ended yet at this point, so we check the current card before calling `fsm.End`.

Replace `endAndSummarize` (lines 369-379):

```go
func (h *Study) endAndSummarize(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int) {
	final, ok := h.fsm.End(userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "quiz.expired", nil), quizCloseKeyboard(loc))
		return
	}
	h.polls.DropForUser(userID)
	roundXP := h.takeRoundXP(userID)
	prog := h.fetchProgress(ctx, userID)
	h.editTo(ctx, b, chatID, msgID, renderQuizSummary(loc, final, prog, h.scoring.DailyGoal, roundXP), quizSummaryKeyboard(loc))
}
```

with:

```go
func (h *Study) endAndSummarize(ctx context.Context, b *bot.Bot, userID int64, loc *goi18n.Localizer, chatID any, msgID int) {
	// Check if we're on a poll card BEFORE ending the FSM (End clears state).
	snap, hasSnap := h.fsm.Snapshot(userID)
	isPoll := hasSnap && !snap.Done() && snap.Current().UIMode == session.QuizUIPoll

	final, ok := h.fsm.End(userID)
	if !ok {
		h.editTo(ctx, b, chatID, msgID, tgi18n.T(loc, "quiz.expired", nil), quizCloseKeyboard(loc))
		return
	}
	h.polls.DropForUser(userID)
	roundXP := h.takeRoundXP(userID)
	prog := h.fetchProgress(ctx, userID)
	summary := renderQuizSummary(loc, final, prog, h.scoring.DailyGoal, roundXP)
	if isPoll {
		b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: msgID})
		h.sendAndTrack(ctx, b, userID, chatIDInt64(chatID), summary, quizSummaryKeyboard(loc))
		return
	}
	h.editTo(ctx, b, chatID, msgID, summary, quizSummaryKeyboard(loc))
}
```

- [ ] **Step 2: Add sendAndTrack helper (plain text, no HTML)**

Add after `sendHTMLAndTrack`:

```go
func (h *Study) sendAndTrack(ctx context.Context, b *bot.Bot, userID int64, chatID int64, text string, kb *models.InlineKeyboardMarkup) {
	sent, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: kb,
	})
	if err != nil {
		h.log.Error("quiz: send msg", "err", err)
		return
	}
	if sent != nil {
		h.setLastMsg(userID, chatID, sent.ID)
	}
}
```

- [ ] **Step 3: Also fix renderState summary branch for poll**

In `renderState` (line 349-355), the `snap.Done()` branch also uses `editTo`. If the user answered a poll and then the FSM is Done, the "Next" callback arrives from the feedback message (which is a normal message, not a poll), so `editTo` will work fine. No change needed here — the feedback message is always a regular message.

- [ ] **Step 4: Build and verify**

Run: `make build`
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/telegram/handlers/study.go
git commit -m "step 56: poll-aware endAndSummarize with delete+send"
```

---

### Task 9: Fix touchLastSeenMiddleware for PollAnswer

**Files:**
- Modify: `internal/telegram/bot.go:309-328`

- [ ] **Step 1: Add PollAnswer case to touchLastSeenMiddleware**

In `touchLastSeenMiddleware` (bot.go line 313-317), the switch currently has two cases. Add a third:

```go
		switch {
		case update.Message != nil && update.Message.From != nil:
			tgID = update.Message.From.ID
		case update.CallbackQuery != nil:
			tgID = update.CallbackQuery.From.ID
		case update.PollAnswer != nil && update.PollAnswer.User != nil:
			tgID = update.PollAnswer.User.ID
		}
```

- [ ] **Step 2: Build and verify**

Run: `make build`
Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add internal/telegram/bot.go
git commit -m "step 56: touch last_seen on poll answer updates"
```

---

### Task 10: Squash into single commit and manual verification

- [ ] **Step 1: Squash commits into one**

Per project convention (one step = one commit), squash all task 1-9 commits into a single commit:

```bash
git reset --soft HEAD~9
git commit -m "step 56: fix-quiz-poll-mode"
```

- [ ] **Step 2: Final build**

Run: `make build`
Expected: clean build, `bin/tg-linguine` produced.

- [ ] **Step 3: Restart bot**

```bash
pkill -f bin/tg-linguine
```

Wait ~70s, then verify:
```bash
ps aux | grep tg-linguine
```
Expected: new PID running.

- [ ] **Step 4: Manual verification in Telegram**

Test each scenario from the spec:

1. `/study` → inline card → answer → feedback replaces in-place (no regression)
2. `/study` → poll card appears with skip/delete/end buttons below
3. Poll card → answer → poll disappears, feedback card with "Next" appears
4. Poll card → skip → poll disappears, skip feedback appears
5. Poll card → end → poll disappears, round summary appears
6. Feedback → next poll: previous feedback deleted, new poll clean
7. Poll card → delete word → poll disappears, delete feedback appears

- [ ] **Step 5: Create task file and close**

```bash
# Create task file
cat > _10_todo/56-fix-quiz-poll-mode.md << 'EOF'
# Step 56: Fix Quiz Poll Mode UX

## Goal
Fix the native Telegram quiz poll mode so polls behave like inline cards:
previous messages are deleted on transitions, control buttons appear under
polls, and the poll is replaced by a feedback card after answering.

## Context
- Spec: docs/superpowers/specs/2026-04-29-fix-quiz-poll-mode-design.md
- Plan: docs/superpowers/plans/2026-04-29-fix-quiz-poll-mode.md

## What to do
See the implementation plan for detailed steps.

## Definition of Done
- [ ] `lastBotMessage` tracking added to Study struct
- [ ] Quiz polls render with skip/delete/end inline buttons
- [ ] HandlePollAnswer deletes poll message before sending feedback
- [ ] handleSkip/handleDelete/endAndSummarize branch for poll UIMode
- [ ] renderState deletes feedback message (not just clears keyboard) before poll
- [ ] touchLastSeenMiddleware handles PollAnswer updates
- [ ] `make build` green
- [ ] Manual verification: all 7 scenarios from spec pass
EOF

# Move to in_progress, execute, then move to done
mv _10_todo/56-fix-quiz-poll-mode.md _20_in_progress/
```
