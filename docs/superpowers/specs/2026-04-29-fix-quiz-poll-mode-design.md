# Fix Quiz (Poll) Mode UX

## Problem

The `/study` quiz uses two UI modes: inline buttons and native Telegram quiz polls. The poll mode has three UX issues:

1. **Previous feedback message stays on screen** when the next card is a poll — `clearKeyboard` removes buttons but leaves text, cluttering the chat.
2. **No control buttons under the poll** — unlike inline mode, the poll has no skip/delete/end buttons, so the user is forced to answer or abandon the round.
3. **Poll stays on screen after answering** — the bot sends feedback below the poll instead of replacing it. The user sees Telegram's green-bar results AND a separate feedback card.

Additionally, `touchLastSeenMiddleware` doesn't extract user ID from `PollAnswer` updates.

## Telegram API Capabilities (confirmed for go-telegram/bot v1.20.0)

| Feature | Supported | Method |
|---|---|---|
| Inline buttons on poll | Yes | `SendPollParams.ReplyMarkup` |
| Delete poll message | Yes | `DeleteMessage()` |
| Edit poll text/options | No | N/A |
| Stop active poll | Yes | `StopPoll()` |

All three requirements are achievable without workarounds.

## Design

### 1. Last-message tracking on `Study` struct

Add `lastMsg map[int64]lastBotMessage` (keyed by userID) with mutex. Stores chatID + messageID of the last bot message sent to each user (poll or feedback).

Methods:
- `setLastMsg(userID, chatID, msgID)` — store after sending
- `takeLastMsg(userID) (lastBotMessage, bool)` — retrieve and clear
- `clearLastMsg(userID)` — clear on round start

This enables any transition to delete the previous message via `DeleteMessage`.

### 2. Control buttons on poll

Add `ReplyMarkup` to `SendPollParams` in `sendCurrentCard()` — a single row with skip/delete/end buttons, same callbacks as inline mode.

### 3. Delete poll after answer

In `HandlePollAnswer()`, call `DeleteMessage(entry.ChatID, entry.MessageID)` before sending the feedback message. Track the feedback message via `setLastMsg`.

### 4. Poll-aware skip/delete/end handlers

`handleSkip`, `handleDelete`, `endAndSummarize` currently use `editToHTML`/`editTo` which fails on poll messages (can't edit poll text). Add a branch: if current card's UIMode is poll → `DropForUser(userID)` + `DeleteMessage` + `SendMessage` (new feedback). Otherwise keep existing edit-in-place logic.

### 5. Clean transitions in `renderState`

Replace `clearKeyboard` with `DeleteMessage` when transitioning to a poll card (line 361-362). The previous feedback message disappears entirely.

### 6. Fix `touchLastSeenMiddleware` in bot.go

Add `update.PollAnswer.User.ID` extraction alongside existing Message and CallbackQuery branches.

## Race conditions

- **Answer + skip simultaneously**: `polls.Take()` is single-shot; `DropForUser` in the callback path provides mutual exclusion. No double-processing possible.
- **DeleteMessage failure**: logged at debug level, non-fatal. Flow continues.

## Files to modify

| File | Changes |
|---|---|
| `internal/telegram/handlers/study.go` | lastMsg tracking, poll ReplyMarkup, delete-before-send in transitions, poll-aware skip/delete/end/renderState |
| `internal/telegram/bot.go` | PollAnswer branch in touchLastSeenMiddleware |

## Verification

1. `/study` → inline card → answer → feedback replaces card in-place (no regression)
2. `/study` → poll card appears with skip/delete/end buttons below
3. Poll card → answer → poll disappears, feedback card with "Next" button appears
4. Poll card → skip → poll disappears, skip feedback appears
5. Poll card → end → poll disappears, round summary appears
6. Feedback → next poll: previous feedback deleted, new poll clean
7. Poll card → delete word → poll disappears, delete feedback appears
