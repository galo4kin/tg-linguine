# Шаг 4. Telegram long-polling скелет (3–4h)

## Цель
Бот отвечает на `/start` строкой «Hello», корректно завершается по
SIGTERM/SIGINT.

## Контекст
Клиент: `github.com/go-telegram/bot` (long-polling). Роутер команд —
встроенный механизм пакета. Никакого webhook, никакого ngrok.

## Действия
1. `internal/telegram/bot.go`: конструктор `New(cfg, log, deps...)`,
   создаёт `*bot.Bot`, регистрирует middleware (логирование апдейтов
   с полями `telegram_user_id`, `language_code`, `update_id`).
2. `internal/telegram/handlers/start.go`: handler возвращает
   `"Hello"` (заглушка, i18n приедет в шаге 5).
3. Регистрация: `b.RegisterHandler(bot.HandlerTypeMessageText, "/start",
   bot.MatchTypeExact, startHandler)`.
4. В `cmd/bot/main.go`: `ctx, cancel := signal.NotifyContext(context.
   Background(), os.Interrupt, syscall.SIGTERM); defer cancel(); b.Start(
   ctx)`. После выхода — `log.Info("shutdown")`.
5. Логировать каждый апдейт на уровне Debug, ошибки — на Error.

## DoD
- `/start` в реальном чате с тестовым ботом отвечает «Hello».
- В `bot.log` для каждого апдейта запись с `telegram_user_id` и
  `language_code`.
- `Ctrl+C` (SIGINT) и `kill` (SIGTERM) — корректное завершение без
  паники, polling останавливается.
- `make build` зелёный.

## Зависимости
Шаг 2.
