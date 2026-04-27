# tg-linguine

Telegram-бот для изучения иностранных языков. Стек: **Go**, SQLite, Groq LLM.

## Требования

- Go 1.22+
- Токен бота от [@BotFather](https://t.me/BotFather)

## Быстрый старт

```bash
make build
cp .env.example .env
# Заполните переменные в .env
make run
```

## Переменные окружения

См. [.env.example](.env.example).

| Переменная | Описание |
|-----------|----------|
| `BOT_TOKEN` | Токен бота (обязательно) |
| `DB_PATH` | Путь к SQLite базе (default: `./bot.db`) |
| `LOG_PATH` | Путь к лог-файлу (default: `./bot.log`) |
| `ENCRYPTION_KEY` | Ключ шифрования |
| `HTTP_TIMEOUT_SEC` | Таймаут HTTP-запросов в секундах |
| `MAX_ARTICLE_SIZE_KB` | Максимальный размер статьи в KB |

## Make-цели

| Команда | Описание |
|---------|----------|
| `make build` | Сборка `bin/tg-linguine` |
| `make run` | Сборка и запуск |
| `make test` | Запуск тестов |
| `make lint` | `go vet` |
| `make tidy` | `go mod tidy` |
