# Step 36 — long-articles-graceful

## Цель

Длинные статьи (например, https://en.wikipedia.org/wiki/Artificial_intelligence) больше не отбиваются ошибкой. Бот всегда даёт пользователю результат — либо разбор целиком, либо выбор между «разобрать начало» и «сжать перед разбором».

План — `~/.claude/plans/cheeky-giggling-gizmo.md`.

## Что делаем

1. **Лимиты в конфиге**:
   - `MAX_ARTICLE_SIZE_KB`: 512 → 4096.
   - `MAX_TOKENS_PER_ARTICLE`: 7000 → 30 000.
   - Поправить комментарии (которые ссылаются на «8K context»).

2. **Статья-сервис** (`internal/articles/`):
   - Удалить `ErrTooLong`, `TooLongError`.
   - Новый тип возврата `AnalyzeResult { Article *AnalyzedArticle; LongPending *LongPending }`.
   - При `tokensEstimated > MaxTokens` вернуть `LongPending{PendingID, Tokens, Words}` вместо ошибки. `pendingStore.Put` с TTL 10 мин.
   - Новый публичный метод `AnalyzeExtracted(ctx, userID, pendingID, mode)` где `mode ∈ {ModeTruncate, ModeSummarize}`. Вызывается из callback-хендлера.
   - В `AnalyzedArticle` добавить поле `Notice string` — баннер для рендера.
   - Новый файл `pending_store.go` (in-memory, TTL).
   - Новый файл `truncate.go` — `truncateAtParagraph(text, maxTokens) (string, percentKept)`.

3. **LLM**:
   - Новый `internal/llm/groq/summarize.go` — `SummarizeToFit(ctx, apiKey, req) (string, error)`.
   - Новый промпт `internal/llm/prompts/summarize.txt` — сжать на языке оригинала, сохранить терминологию.
   - Поддержка в stub-провайдере (`internal/llm/stub`) для тестов.
   - Если вход всё равно > ~110K токенов — заранее усечь.

4. **Telegram**:
   - `internal/telegram/handlers/url.go`: при `LongPending != nil` — отправить сообщение с двумя inline-кнопками вместо ошибки.
   - Новый `internal/telegram/handlers/long_article.go` с `CallbackPrefixLongArticle = "art:long:"`. Обрабатывает `art:long:<id>:t` и `art:long:<id>:s`. По истечении TTL — дружелюбное сообщение.
   - Регистрация в `internal/telegram/bot.go`.
   - Renderer прокидывает `Notice` сверху ответа.

5. **i18n** (`internal/i18n/locales/{en,ru,es}.yaml`):
   - Удалить `article.err.too_long`.
   - Добавить: `article.long.prompt`, `article.long.btn.truncate`, `article.long.btn.summarize`, `article.long.banner.truncated`, `article.long.banner.summarized`, `article.long.expired`.

## Тесты

- `internal/articles/truncate_test.go` — границы абзаца, бюджет, процент.
- `internal/articles/pending_store_test.go` — Put/Take/TTL/конкурентность.
- `internal/articles/usecase_test.go` — короткая (как раньше), длинная (LongPending), ModeTruncate, ModeSummarize.
- `internal/telegram/handlers/long_article_test.go` — оба варианта callback + истёкший id.
- `internal/telegram/handlers/url_integration_test.go` — длинная статья → промпт с кнопками, не ошибка.
- `internal/i18n/locales_consistency_test.go` — проходит.

## DoD

- [ ] `make build` зелёный.
- [ ] `make test` зелёный.
- [ ] Единый коммит `step 36: long-articles-graceful`.
- [ ] `pkill -f bin/tg-linguine`, через ~70 сек живой новый PID.
- [ ] Файл переехал в `_30_done/`.
