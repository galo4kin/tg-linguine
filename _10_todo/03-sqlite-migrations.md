# Шаг 3. SQLite + миграции (3–4h)

## Цель
Подключение SQLite (pure Go) и автоматический прогон миграций при
старте.

## Контекст
Драйвер: `modernc.org/sqlite` (без CGO). Миграции:
`github.com/golang-migrate/migrate/v4` с `source/iofs` и `embed.FS`.
PRAGMA: `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`,
`synchronous=NORMAL`.

## Действия
1. `internal/storage/sqlite.go`: `Open(path string) (*sql.DB, error)` —
   открывает БД, выставляет PRAGMA, возвращает `*sql.DB`.
2. `migrations/0001_init_users.up.sql` + `.down.sql`:
   ```sql
   CREATE TABLE users (
     id INTEGER PRIMARY KEY AUTOINCREMENT,
     telegram_user_id INTEGER NOT NULL UNIQUE,
     created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
   );
   CREATE INDEX idx_users_tg ON users(telegram_user_id);
   ```
3. `internal/storage/migrate.go`: `//go:embed migrations/*.sql` — нет,
   embed-каталог должен лежать рядом с пакетом. Перенести SQL внутрь
   пакета `internal/storage/migrations/` или встраивать через
   отдельный пакет `internal/storage/migrations`. Реализовать
   `RunMigrations(db *sql.DB, log *slog.Logger) error`.
4. В `cmd/bot/main.go` после загрузки конфига: `db, err := storage.Open(
   cfg.DBPath)`; `defer db.Close()`; `storage.RunMigrations(db, log)`.
5. Залогировать применённую версию миграции.

## DoD
- При первом запуске создаётся файл по `DB_PATH`, в нём таблица
  `users` и `schema_migrations`.
- При повторном запуске миграции не падают (idempotent).
- Удаление БД и перезапуск воссоздают всё с нуля.
- `make build` зелёный.
- В логах строка с применённой версией миграции.

## Зависимости
Шаг 2.
