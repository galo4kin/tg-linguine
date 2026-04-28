# Step 39 — groq-schema-retry-and-429-diag

## Проблема

После step 38: первый chat-вызов в Groq идёт 200 (`groq.chat ok`), затем сразу `long: analyze failed: llm: rate limited`. Тела 429 в логах нет: я снапшотил только `default`-ветку статусов.

Гипотеза: первый JSON-ответ не прошёл schema-валидацию (article 5.9K + 3K output cap → JSON обрезается на словах/adapted versions), запускается внутренний retry в `chat`. Второй вызов в той же TPM-минуте получает 429 и валится.

## Что делаем

1. `groq/analyze.go` и `groq/summarize.go`: снапшотить тело ответа также для 401/403/429, не только для default. Body логируется + кладётся в текст ошибки.
2. `groq/analyze.go`: WARN-лог при schema-retry с первыми ~500 байтами невалидного ответа модели (`groq.analyze schema-retry`).
3. `analyzeMaxCompletionTokens`: 3000 → **4000** — JSON-ответ часто переполняет 3K при наличии 3 adapted versions + words.
4. `MAX_TOKENS_PER_ARTICLE` envDefault и `.env`: 6000 → **5000**, чтобы 5K input + 1K system + 4K output = 10K с запасом 2K под `TPM=12000`.
5. README синхронизировать.

## DoD

- `make build && make test` зелёные.
- Один коммит `step 39: groq-schema-retry-and-429-diag`.
- `pkill -f bin/tg-linguine`, watchdog поднимает свежий PID.
- В логах при следующем 429/401/403 видно тело ответа.
- Wikipedia AI: park → truncate → analyze идёт без retry / без 429.
