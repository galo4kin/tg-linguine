# Step 37 — fix-413-token-budget

## Проблема

После step 36 длинные статьи (Wikipedia AI) парковались, пользователь жмёт «Разобрать начало» или «Сжать и разобрать», но получает «Не удалось связаться с Groq. Попробуйте позже.»

В логах:
```
"article truncated","percent_kept":66
"long: analyze failed","err":"llm: provider unavailable: status 413"
```

HTTP 413 = Request Entity Too Large. Truncate оставил 66% от 45 330 токенов ≈ **30 000 токенов**, но Groq free tier для `llama-3.3-70b-versatile` 413-ит запросы такого размера. Контекст модели 128K — это потолок paid-tier; free tier зажимает per-request input гораздо ниже (порядок 10–12K).

## Что делаем

1. `MAX_TOKENS_PER_ARTICLE` envDefault: 30000 → **10000** ([config.go](internal/config/config.go)). 
2. `summarizeInputBudget` const: 100000 → **12000** ([usecase.go](internal/articles/usecase.go)) — вход в summarize тоже под cap'ом free tier.
3. При не-2xx ответе Groq читать первые ~500 байт тела и класть в текст ошибки + INFO-лог, чтобы 413/4xx-ошибки были диагностируемы. Затрагивает `chat` и `chatPlainText` в [internal/llm/groq/](internal/llm/groq/).
4. README синхронизировать с новыми дефолтами и пояснением про Groq tiers.

## DoD

- `make build` зелёный.
- `make test` зелёный.
- В логах при 413 видно первые 500 байт ответа Groq.
- Один коммит `step 37: fix-413-token-budget`.
- `pkill -f bin/tg-linguine` → watchdog поднимает новый PID.
