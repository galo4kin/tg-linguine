# Шаг 2. Конфигурация и логирование (2–3h)

## Цель
Загрузка `.env` + structured-логи с ротацией. Бот падает с понятным
сообщением, если не хватает обязательных переменных.

## Контекст
Используем `caarlos0/env` + `joho/godotenv` для конфига; `slog` (stdlib)
+ `gopkg.in/natefinch/lumberjack.v2` для логов и ротации.

## Действия
1. `internal/config/config.go`: структура `Config` с тегами `env:""`.
   Поля: `BotToken` (required), `DBPath`, `LogPath`, `EncryptionKey`
   (required, base64 32 байта), `HTTPTimeoutSec`, `MaxArticleSizeKB`,
   `LogLevel` (info по умолчанию), `LogMaxSizeMB` (10),
   `LogMaxBackups` (5), `LogMaxAgeDays` (30).
2. Функция `Load()` — `godotenv.Load()` (best-effort) → `env.Parse()`
   → возвращает `*Config` или ошибку с перечнем недостающих полей.
3. `internal/logger/logger.go`: `New(cfg *config.Config) *slog.Logger`
   — JSON handler на `lumberjack.Logger{Filename: cfg.LogPath, ...}`,
   уровень из `cfg.LogLevel`. Никаких принтов в stdout в release — но
   в dev можно дублировать (см. ENV `LOG_STDOUT=1`, опционально).
4. В `cmd/bot/main.go`: `cfg, err := config.Load(); if err → fmt.Fprintln(
   os.Stderr, ...); os.Exit(1)`. Затем `log := logger.New(cfg)` и
   `log.Info("boot", "version", ...)` — на этом пока всё.
5. Обновить `.env.example` — описать каждое поле комментарием.

## DoD
- Запуск без `BOT_TOKEN` или `ENCRYPTION_KEY` → процесс выходит с
  кодом 1 и человекочитаемым сообщением в stderr (не паникой).
- При нормальном `.env` — лог-запись в `bot.log` в JSON.
- Ротация настроена (видно в коде; реальный rollover вручную не
  проверяем).
- `make build` зелёный, `go vet ./...` без ошибок.
- Секреты (`BotToken`, `EncryptionKey`) НЕ попадают в лог при печати
  конфига.

## Зависимости
Шаг 1.
