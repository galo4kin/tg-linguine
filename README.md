# tg-linguine

Telegram-бот для изучения иностранных языков: пользователь присылает
URL статьи, бот скачивает её, прогоняет через Groq LLM и отдаёт обратно
карточку статьи с резюме на родном/целевом языке, адаптированными
версиями под соседние CEFR-уровни и постраничным словарём по
встретившимся словам. Стек: **Go** (single binary, без CGO), SQLite
(`modernc.org/sqlite`), [`go-telegram/bot`](https://github.com/go-telegram/bot)
поверх long-polling, Groq через OpenAI-совместимый клиент.

## Требования

- Go **1.26.2+** (см. `go.mod`)
- Токен бота от [@BotFather](https://t.me/BotFather)
- Groq API key — выдаётся каждым пользователем индивидуально через
  `/setkey`, в `.env` бота не нужен

## Быстрый старт

```bash
make build
cp .env.example .env
# Заполните BOT_TOKEN и ENCRYPTION_KEY (см. ниже)
make run
```

## Переменные окружения

Полный canonical-список — [.env.example](.env.example). Все переменные
читаются из окружения (или из `.env` в рабочей директории) через
[`caarlos0/env`](https://github.com/caarlos0/env).

| Переменная | Default | Описание |
|-----------|---------|----------|
| `BOT_TOKEN` | — (required) | Токен бота от BotFather |
| `ENCRYPTION_KEY` | — (required) | Master-ключ для AES-GCM шифрования API-ключей пользователей в БД (см. отдельный раздел) |
| `DB_PATH` | `./bot.db` | Путь к файлу SQLite |
| `LOG_PATH` | `./bot.log` | Путь к лог-файлу (ротация — `lumberjack`) |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `LOG_MAX_SIZE_MB` | `10` | Размер лог-файла, после которого ротация |
| `LOG_MAX_BACKUPS` | `5` | Сколько ротированных файлов хранить |
| `LOG_MAX_AGE_DAYS` | `30` | Максимальный возраст ротированного файла |
| `LOG_STDOUT` | `false` | Дублировать ли логи в stdout (удобно в dev) |
| `HTTP_TIMEOUT_SEC` | `20` | Таймаут HTTP — общий для extractor'а и Groq-клиента |
| `MAX_ARTICLE_SIZE_KB` | `4096` | Лимит размера скачанной страницы для readability-extractor (Wikipedia featured articles бывают 1–3 MB сырого HTML) |
| `GROQ_MODEL` | `llama-3.3-70b-versatile` | Имя модели в чат-комплишн вызовах Groq (контекст 128K) |
| `RATE_LIMIT_PER_HOUR` | `10` | Лимит обрабатываемых URL на пользователя в час |
| `MAX_TOKENS_PER_ARTICLE` | `6000` | Гейт по эвристике `runes/4`. Если статья длиннее — бот предлагает выбрать: разобрать первую часть (truncation) или сжать перед разбором (LLM pre-summary). Дефолт подобран под Groq free tier (TPM cap = 12000 на запрос, считается input + reserved output): 6K статьи + 3K зарезервированного JSON-ответа + ~1K system prompt ≈ 10K. На paid tier этот лимит можно поднимать. |
| `ADMIN_USER_ID` | `0` | Telegram-id админа (см. раздел «Admin»). `0` = админ-функции выключены |

## Make-цели

| Команда | Описание |
|---------|----------|
| `make build` | Сборка `bin/tg-linguine` |
| `make run` | Сборка и запуск (с подгрузкой `.env`) |
| `make test` | `go test ./...` |
| `make lint` | `go vet ./...` |
| `make tidy` | `go mod tidy` |

## Миграции

SQL-миграции лежат в `internal/storage/migrations/` и встраиваются в
бинарь через `embed`. На старте `cmd/bot/main.go` вызывает
`storage.RunMigrations`, который через `golang-migrate` накатывает все
непрожатые версии до открытия Telegram-сессии. Отдельной CLI-команды
нет — деплой = `git pull && make build && (watchdog поднимет новый pid)`.

## Команды бота

Регистрируются в `internal/telegram/bot.go`. Все они доступны любому
пользователю — отдельной роли админа в текущей версии нет.

| Команда | Что делает | Кому |
|---------|-----------|------|
| `/start` | Запускает онбординг: язык изучения → CEFR-уровень → подсказка про `/setkey` | все |
| `/setkey` | Принимает в следующем сообщении Groq API key, валидирует его, шифрует и сохраняет (входящее сообщение тут же удаляется) | все |
| `/history` | Постраничная история ранее проанализированных статей с возможностью открыть карточку | все |
| `/settings` | Меню настроек: смена интерфейсного / целевого языка, CEFR, ввод нового API-ключа, удаление аккаунта | все |
| `/mywords` | Постраничный список выученных и активных слов с фильтром по статусу | все |
| `/study` | Флэшкарточный режим повторения по словам со статусом `learning` | все |
| `/delete_me` | Полное удаление пользователя и всех его данных (с подтверждением) | все |
| `/stats` | Сводка по числу пользователей (всего / активных за 24ч / 7д), статей (всего / за сутки) и слов в словаре | админ |
| `/whoami` | Возвращает роль (`admin`/`user`) и Telegram-id отправителя | все |
| `/shutdown` | Корректно завершает процесс — watchdog поднимет новый pid в течение минуты | админ |

> Команды с пометкой `админ` молча игнорируют сообщения от
> не-админов: ответ не приходит, бот не «светит» наличие админ-режима.
> `/whoami` доступен всем — это диагностический инструмент, чтобы
> можно было быстро убедиться, что `ADMIN_USER_ID` подхватился.

Кроме slash-команд бот принимает:

- любое сообщение, содержащее `http(s)://…` URL — запускает pipeline
  «extract → analyze → store → отдать карточку» (учитывая
  `RATE_LIMIT_PER_HOUR` и `MAX_TOKENS_PER_ARTICLE`);
- следующее текстовое сообщение после `/setkey` — трактуется как API-ключ
  и сразу удаляется из чата;
- callback-кнопки на инлайн-клавиатурах: пагинация словаря, смена
  статуса слова, навигация по `/history`, ответы в `/study` и т.п.

## Admin

Бот поддерживает одного админа — Telegram-пользователя, чей id равен
`ADMIN_USER_ID`. Без этой переменной (или со значением `0`) админ-роль
выключена целиком: админ-команды и стартовый пинг просто не
существуют, гейт `IsAdmin` всегда возвращает `false`.

Узнать свой Telegram id — написать [@userinfobot](https://t.me/userinfobot).
Он ответит JSON'ом с числовым `id`. Положить это значение в
`ADMIN_USER_ID` в `.env` и перезапустить бота.

На старте лог пишет `admin configured user_id=…` или `admin disabled`
— это самый быстрый способ проверить, что переменная подхватилась.

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

### Раскатка новой версии

```bash
cd ~/Projects/tg-linguine
git pull
make build
kill $(pgrep -f bin/tg-linguine)   # SIGTERM, не SIGKILL — даём дренироваться
```

В течение минуты watchdog запустит новый процесс. Миграции применятся
сами на старте.

### Smoke-тест

После добавления крон-строки:

1. В Telegram: `/start` → онбординг → `/setkey` с реальным Groq API
   key → отправить URL статьи → дождаться анализа и пагинации словаря.
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
