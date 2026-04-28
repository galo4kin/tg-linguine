# Step 47: Two word-related buttons on the article card

## Goal
Split the single "Показать все слова" button on the article card into two buttons:
- **"Разобрать новые слова"** — opens the article's word list (what "Показать все слова" does today)
- **"Показать все слова"** — starts the flashcard study session for the user's learning-status words

## What to do

### A. Add i18n key `article.card.new_words` to all three locale files

`internal/i18n/locales/ru.yaml`:
```yaml
article.card.new_words: "Разобрать новые слова"
```

`internal/i18n/locales/en.yaml`:
```yaml
article.card.new_words: "Review new words"
```

`internal/i18n/locales/es.yaml`:
```yaml
article.card.new_words: "Repasar palabras nuevas"
```

### B. `articleCardKeyboard()` in `internal/telegram/handlers/card.go` (around lines 168–173)

Replace the single "show all words" row with two rows:
```go
if totalWords > 0 {
    rows = append(rows, []models.InlineKeyboardButton{{
        Text:         tgi18n.T(loc, "article.card.new_words", nil),
        CallbackData: fmt.Sprintf("%s%d:0", CallbackPrefixWords, article.ID),
    }})
}
rows = append(rows, []models.InlineKeyboardButton{{
    Text:         tgi18n.T(loc, "article.card.show_all_words", nil),
    CallbackData: CallbackPrefixStudy + "start",
}})
```

`CallbackPrefixStudy` is defined in `internal/telegram/handlers/study.go` as `"study:"`.
Import it in card.go if needed (it's in the same package, so no import required).

### C. `Study.HandleCallback()` in `internal/telegram/handlers/study.go`

Add a `"start"` case to the existing switch (after the `"close"` and `"end"` cases):

```go
case payload == "start":
    deck, ok := h.buildDeck(ctx, u.ID, loc)
    if !ok {
        b.SendMessage(ctx, &bot.SendMessageParams{
            ChatID: chatID,
            Text:   tgi18n.T(loc, "study.empty", nil),
        })
        return
    }
    h.fsm.Start(u.ID, deck)
    snap, _ := h.fsm.Snapshot(u.ID)
    b.SendMessage(ctx, &bot.SendMessageParams{
        ChatID:      chatID,
        Text:        renderStudyCard(loc, snap),
        ReplyMarkup: studyCardKeyboard(loc, snap.Current().DictionaryWordID),
    })
```

This sends a fresh message with the first flashcard; leaves the article card message intact.

Note: `h.buildDeck()` returns `([]session.StudyCard, bool)`. The existing `HandleCommand`
method at line 66 shows how it's called.

## Definition of done
- [ ] `article.card.new_words` added to ru/en/es locale files
- [ ] "Разобрать новые слова" button on card opens article words (only when totalWords > 0)
- [ ] "Показать все слова" button on card starts flashcard study (always shown)
- [ ] `study:start` payload handled in `Study.HandleCallback`
- [ ] `make build` passes
- [ ] Locale consistency test passes: `make test` (if test suite includes i18n consistency check)
- [ ] Commit `step 47: card-two-word-buttons`
