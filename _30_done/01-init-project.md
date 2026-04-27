# Шаг 1. Инициализация Go-проекта (3–4h)

## Цель
Скелет Go-проекта `tg-linguine`, собирается под darwin/arm64, запускается.

## Контекст
Текущий репозиторий ошибочно инициализирован как TypeScript+grammY
(`package.json`, `src/index.ts`, `tsconfig.json`). Стек переезжает на
Go — package-by-feature под `/internal/` (см. `CLAUDE.md`).

Аналог-ориентир: `~/Projects/tg-boltun` (Go, flat). Берём оттуда
референс на `.gitignore`, Makefile, watchdog-скрипт. Структуру делаем
свою — package-by-feature, не flat.

## Действия
1. Снести TS-артефакты: `package.json`, `tsconfig.json`, `src/`,
   `node_modules/` (если появились).
2. `go mod init github.com/<owner>/tg-linguine` (уточнить владельца у
   пользователя, если неясно — поставить плейсхолдер
   `github.com/nikita/tg-linguine`).
3. Создать структуру:
   ```
   cmd/bot/main.go            // минимальный main, печатает "tg-linguine boot"
   internal/
     config/                  // пусто — заполнится в шаге 2
     logger/
     storage/
     telegram/
     i18n/
     users/
     articles/
     dictionary/
     llm/
     crypto/
     session/
   migrations/                // .gitkeep
   locales/                   // .gitkeep
   configs/                   // .gitkeep
   ```
4. `Makefile` с целями `build`, `run`, `test`, `lint`, `tidy`. Цель
   `build` собирает `bin/tg-linguine` под текущую платформу. Цель
   `lint` зовёт `go vet` (golangci-lint опционален — не блокировать).
5. `.gitignore`: `bin/`, `*.db`, `*.db-journal`, `*.db-wal`, `*.db-shm`,
   `bot.log*`, `.env`, `.env.local`, `.DS_Store`, `vendor/`.
6. `.env.example` с пустыми ключами на будущее: `BOT_TOKEN=`,
   `DB_PATH=./bot.db`, `LOG_PATH=./bot.log`, `ENCRYPTION_KEY=`,
   `HTTP_TIMEOUT_SEC=20`, `MAX_ARTICLE_SIZE_KB=512`.
7. `README.md` — короткое описание, как собрать (`make build`) и
   запустить (`make run`).
8. Коммит: `init: Go skeleton (package-by-feature)`.

## DoD
- В корне нет TS-артефактов.
- `go mod tidy` проходит без ошибок.
- `make build` создаёт `bin/tg-linguine` без ошибок (под darwin/arm64
  на mac, под linux/amd64 — кросс-сборка опциональна, не требуется
  здесь).
- `make run` запускает бинарник и печатает «tg-linguine boot» (или
  аналогичную заглушку), затем завершается чисто.
- `git status` чистый после коммита.

## Зависимости
—
