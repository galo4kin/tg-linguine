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
| `ENCRYPTION_KEY` | Master-ключ шифрования API-ключей пользователей (см. ниже) |
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

## Деплой на mac mini

Бот живёт прямо в директории репозитория (`~/Projects/tg-linguine` на
текущей mac mini), бинарь — `bin/tg-linguine`, БД и логи рядом.
Автоперезапуск — через cron-watchdog `scripts/linguine-watchdog.sh`.

### Первичная настройка

```bash
cd ~/Projects/tg-linguine
cp .env.example .env              # заполнить BOT_TOKEN и ENCRYPTION_KEY
make build                        # собрать bin/tg-linguine
chmod +x scripts/linguine-watchdog.sh
```

`ENCRYPTION_KEY` сгенерировать один раз:

```bash
openssl rand -base64 32
```

> Бэкап `ENCRYPTION_KEY` обязателен — храните его в менеджере паролей
> или зашифрованном томе, отдельно от репозитория. Потеря ключа =
> невозможность расшифровать сохранённые ключи всех пользователей.

### Crontab

`crontab -e` и добавить одну строку:

```
* * * * * LINGUINE_DIR=~/Projects/tg-linguine LINGUINE_LOG=~/Projects/tg-linguine/watchdog.log ~/Projects/tg-linguine/scripts/linguine-watchdog.sh
```

Watchdog раз в минуту:
- если процесс `bin/tg-linguine` не найден — стартует его через `nohup`;
- если найдено больше одного — убивает все и запускает один;
- иначе ничего не делает.

Логи watchdog'а пишутся в `watchdog.log` (gitignored), логи самого
бота — в `bot.log` через `lumberjack` (ротация по размеру).

### Smoke-тест

После добавления крон-строки:

1. В Telegram: `/start` → онбординг → ввести Groq API key → отправить
   реальный URL статьи → дождаться анализа и пагинации словаря.
2. `kill -9 $(pgrep -f bin/tg-linguine)` → подождать ≤1 минуты →
   `pgrep -f bin/tg-linguine` снова возвращает pid.
3. `sudo reboot` → после загрузки в течение минуты бот снова в живых.

### Бэкап `bot.db`

База данных живёт в `~/Projects/tg-linguine/bot.db` (или по `DB_PATH`).
Минимум — горячий онлайновый снапшот через sqlite3:

```bash
sqlite3 ~/Projects/tg-linguine/bot.db ".backup '/Volumes/Backup/bot-$(date +%F).db'"
```

`.backup` — атомарный, безопасен поверх работающего бота. Кладите в
ежедневный cron на отдельный диск/облако, ротируйте по дате.
`ENCRYPTION_KEY` бэкапится отдельно — без него зашифрованные API-ключи
из БД восстановить нельзя.

### Graceful shutdown

При `SIGTERM` (через `kill` без `-9`, `launchctl unload`, или просто
рестарт mac mini) бот останавливает long-poll, дожидает текущие
обработчики до 30s и выходит. Если за 30s обработчики не успели —
лог содержит `shutdown: drain timeout exceeded` и процесс выходит
принудительно (новый процесс поднимет watchdog).

### Observability

Логи бота — структурированный JSON через `slog` + ротация
`lumberjack`. Полезные ключи для grep:

- `analysis_duration_ms`, `tokens_estimated`, `article_chars`,
  `cache_hit` — каждый успешный/закешированный анализ;
- `groq_retries` — сколько раз ретраили Groq на 5xx;
- `errors_total` — фиксируется на каждой error-записи (panic в
  хендлере, сбой шатдауна, провал groq-запроса).

Ничего из этого никуда не отправляется по сети — для дашборда натравите
log-scraper (Promtail/Vector/Loki) на `bot.log`.

## Master-ключ шифрования (`ENCRYPTION_KEY`)

API-ключи пользователей (Groq и т.п.) хранятся в БД зашифрованными
AES-256-GCM. Мастер-ключ берётся из переменной окружения
`ENCRYPTION_KEY` — это **32 байта в base64** (например,
`openssl rand -base64 32`).

> **Внимание.** Потеря `ENCRYPTION_KEY` означает невозможность
> расшифровать сохранённые ключи всех пользователей: их придётся
> вводить заново. Сгенерируйте мастер-ключ один раз при первом
> деплое и храните его в надёжном защищённом backup'е (например,
> менеджер паролей или зашифрованный том). Никогда не коммитьте
> `.env` в репозиторий.
