# Шаг 29. Hardening и observability lite (3–4h)

## Цель
Прод-готовность: бот не падает на ошибках LLM, переживает SIGTERM
с дренированием, метрики в логах.

## Действия
1. Panic recovery в Telegram-handlers: middleware-обёртка `func(next)
   { defer recover() ... }`. Залогировать stacktrace, ответить
   пользователю i18n-сообщением «Что-то пошло не так».
2. Retry для Groq: `internal/llm/groq/client.go` — макс. 2 попытки
   с экспоненциальным backoff (1s, 3s) на 5xx и сетевые ошибки.
   На 4xx (401, 400) — без retry.
3. Graceful shutdown: при SIGTERM `context.Cancel` по верху →
   `bot.Stop()` → ожидание активных хендлеров с таймаутом 30s
   (`sync.WaitGroup` с `WaitTimeout`).
4. Метрики в логах (структурированно): `analysis_duration_ms`,
   `article_chars`, `tokens_estimated`, `cache_hit`,
   `groq_retries`, `errors_total`.
5. README дополнить: deploy-инструкция, генерация ENCRYPTION_KEY,
   crontab, бэкап `bot.db`.

## DoD
- Убитый Groq (например, тест с заведомо невалидным endpoint) →
  бот ловит, отвечает, не падает.
- SIGTERM во время активного анализа → анализ дорабатывает или
  таймаутится за ≤30s, бинарник выходит чисто.
- Лог содержит метрики из списка выше.
- README покрывает deploy.
- `make build` + `make test` зелёные.

## Зависимости
Шаг 28.
