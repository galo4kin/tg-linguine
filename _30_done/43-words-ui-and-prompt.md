# Step 43 — words UI cleanup + richer LLM word selection

## Goal

Improve the "words to learn" screen opened from an article card and nudge the
LLM to surface a useful number of vocabulary candidates.

## Why

User feedback on the current screen:

- A `[◀ Назад][Вперёд ▶]` row is shown even when the entire word list fits on
  one page (`totalPages == 1`), wired to `words:noop`. Pure visual noise.
- The word block is plain text with no Telegram formatting — dense and hard
  to scan.
- A B2 article that should yield ~10–20 vocabulary candidates returned only
  3. There is no quantity guidance in the LLM system prompt at all, so Groq
  is being stingy.

## What to do

### 1. Hide pagination when single page

`internal/telegram/handlers/words.go`, `wordsKeyboard()`:

- Wrap the prev/next row in `if totalPages > 1`. Drop the `noop` branches
  inside the conditional (no longer reachable from the new keyboard).
- Keep the `words:noop` handler in `HandleCallback` for backwards
  compatibility with already-rendered messages.
- Close button row stays unconditional.

### 2. Telegram HTML formatting for the words block

`internal/telegram/handlers/words.go`:

- Add a small `htmlEscape` helper (replacer for `&`, `<`, `>`).
- Add `models.ParseModeHTML` to both `EditMessageText` calls in
  `renderPage()` (the empty-state and the normal render).
- New per-word layout in `renderWordsPage()`:

  ```
  <b>{i}. {surface}</b> · <i>{lemma}, {pos}</i> · <code>{ipa}</code>
  → <b>{translation}</b>
  <i>{example_target}</i>
  {example_native}
  ```

  Blank line between entries.

- Header:
  - When `totalPages > 1`: existing `words.page_header` wrapped in `<b>`.
  - When `totalPages == 1`: new i18n key `words.header_single` wrapped in
    `<b>` — `{{.Total}}` only.

### 3. Add `words.header_single` to all three locales

`internal/i18n/locales/{en,ru,es}.yaml`:

- en: `New words — {{.Total}}`
- ru: `Новые слова — {{.Total}}`
- es: `Palabras nuevas — {{.Total}}`

### 4. Expand the LLM system prompt

`internal/llm/prompts/system.txt`:

Replace the existing single-line word-selection guidance with explicit
quantity guidance — target ranges by article length, prefer content words,
do not pad. No JSON-schema change.

## Definition of Done

- Single-page words screen renders without prev/next row; multi-page screen
  still has it.
- Header reads "Новые слова — N" on a single page, "Слова K/N" on multi-page.
- Word entries render with bold surface form, italic lemma+POS, monospace
  IPA, bold translation, italic target-language example.
- `make build` is green.
- `make test` is green.
- Task file moved to `_30_done/`, single commit `step 43:
  words-ui-and-prompt`.
- `pkill -f bin/tg-linguine` so watchdog respawns the new binary.
