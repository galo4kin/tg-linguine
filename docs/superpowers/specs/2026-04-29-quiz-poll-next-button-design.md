# Quiz Poll: "Next" Button + Config Toggle

## Problem

Telegram does not deliver `poll_answer` updates to the bot (confirmed: zero poll_answer entries in the entire bot log history). This makes `HandlePollAnswer` dead code — the bot never learns that a user answered a quiz poll, so it cannot delete the poll or show feedback.

## Solution

Replace the "Skip" button under polls with a "Next" button. This uses `callback_query` (which works reliably) instead of `poll_answer` (which doesn't arrive). "Next" always acts as a skip — poll cards become visual variety without affecting XP/mastery.

Add a `QUIZ_POLL_ENABLED` config flag (default: `true`) so the poll mode can be toggled from `.env`.

## Design

### 1. Config flag

Add `QuizPollEnabled bool` to `Config` struct with `env:"QUIZ_POLL_ENABLED" envDefault:"true"`. Pass it through to the Study handler via `QuizScoring` (or a new field on Study).

When disabled, `PickQuizUIMode()` always returns `QuizUIInline`.

### 2. Poll control buttons

Change from: **Skip / Delete / End**
Change to: **Next / Delete / End**

"Next" uses callback `study:next` (same as the existing feedback "Next" button).

### 3. Handle "Next" on poll cards

In `HandleCallback`, the `study:next` case currently calls `renderState`. Add a check: if the FSM's current card is a poll (cursor not yet advanced), call `RecordSkip` first, then delete the poll message, then show the next card.

This requires checking the FSM snapshot BEFORE advancing — if `snap.Current().UIMode == QuizUIPoll`, the user is clicking "Next" on a poll (not on a feedback message).

### 4. Remove dead poll_answer code

- Delete `HandlePollAnswer` and `MatchPollAnswer` from `study.go`
- Delete `session/quiz_polls.go` entirely (`QuizPolls`, `QuizPollEntry`)
- Remove handler registration and `poll_answer` from `AllowedUpdates` in `bot.go`
- Remove `polls` field from `Study` struct and `NewStudy` parameter
- Remove all `h.polls.*` calls throughout `study.go`
- Remove `PollAnswer` branch from `touchLastSeenMiddleware`
- Remove `quizPolls` creation in `bot.go`

### 5. Simplify poll-aware handlers

With no `polls` registry, `handleSkip` and `handleDelete` poll branches simplify: no `DropForUser` calls needed.

## Files to modify

| File | Changes |
|---|---|
| `internal/config/config.go` | Add `QuizPollEnabled` field |
| `internal/session/quiz.go` | `PickQuizUIMode` accepts config flag |
| `internal/session/quiz_polls.go` | Delete entirely |
| `internal/telegram/handlers/study.go` | Remove polls, add "Next" on poll handling, rename button |
| `internal/telegram/bot.go` | Remove quizPolls, poll_answer registration, AllowedUpdates cleanup |

## Verification

1. `QUIZ_POLL_ENABLED=true`: `/study` → poll cards appear with Next/Delete/End buttons
2. Poll card → click "Next" → poll disappears, next card shown
3. Poll card → click "Delete" → poll disappears, word deleted, next card shown
4. Poll card → click "End" → poll disappears, summary shown
5. `QUIZ_POLL_ENABLED=false`: `/study` → all cards are inline, no polls
6. Inline cards work exactly as before (no regression)
