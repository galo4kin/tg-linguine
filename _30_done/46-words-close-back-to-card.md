# Step 46: "Закрыть" on words screen navigates back to article card

## Goal
Pressing "Закрыть" on the words screen currently deletes the message. Instead it should
re-render the article card in-place (same message, default view).

## What to do

### 1. `wordsKeyboard()` in `internal/telegram/handlers/words.go` (around line 369)

Change the close button callback from `words:close` to the card-view callback that
`Card.HandleCallback` already handles:

```go
// Before:
closeBtn := models.InlineKeyboardButton{Text: closeText, CallbackData: CallbackPrefixWords + "close"}

// After:
closeBtn := models.InlineKeyboardButton{
    Text:         closeText,
    CallbackData: fmt.Sprintf("%sv:%d:c:t", CallbackPrefixCard, articleID),
}
```

`art:v:<id>:c:t` = current level (`c`), target-language summary (`t`) — the default card view.

### 2. Remove dead `words:close` branch in `HandleCallback()` (around lines 88–91)

Delete these lines since they can no longer be reached:
```go
if data == "close" {
    b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: msgID})
    return
}
```

## Definition of done
- [ ] Close button on words screen uses `art:v:<id>:c:t` callback
- [ ] Dead `words:close` handler branch removed
- [ ] `make build` passes
- [ ] Commit `step 46: words-close-back-to-card`
