# Step 40 — groq-respect-retry-after

## Проблема

После step 39 в логе видна реальная причина 429: при «Сжать и разобрать» summarize использует ~9934 из 12K TPM, аналитический вызов сразу за ним просит ещё 4016 → 9934+4016=13950 > 12000 → 429. Тело Groq:

```
"Rate limit reached ... Limit 12000, Used 9934, Requested 4016. Please try again in 9.75s."
```

Это не баг «съел всё за один запрос» — это TPM по 60-секундному скользящему окну. Лекарство Groq указывает прямо: подождать ~10 секунд.

## Что делаем

1. В `groq/client.go` добавить helper `parseRateLimitRetryAfter(headers, body) time.Duration`:
   - Сначала смотреть `Retry-After` header (RFC 6585: целое количество секунд или HTTP-date).
   - Если нет — регуляркой выдрать `try again in (\d+(?:\.\d+)?)s` из JSON-тела.
   - Возвращать 0 если не нашли (наш сигнал «не ретраим»).
   - Капать сверху 60 секундами на всякий случай.
2. В `chat()` и `chatPlainText()`: при 429 — если parseRateLimitRetryAfter > 0, поспать и повторить запрос **один раз**. Если повтор тоже 429 — отдать `ErrRateLimited` как раньше.
3. Добавить unit-тест в `internal/llm/groq/` на `parseRateLimitRetryAfter` с вариантами тел Groq (header, body, ничего).
4. Лог `groq.chat rate-limit retry` с указанием wait.

## DoD

- `make build && make test` зелёные.
- Один коммит `step 40: groq-respect-retry-after`.
- `pkill -f bin/tg-linguine`, watchdog поднимает свежий PID.
- Wikipedia AI: «Сжать и разобрать» → summarize ok → ждёт ~10s → analyze ok → карточка.
