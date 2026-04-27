# Шаг 6. User repository + регистрация (3–4h)

## Цель
При первом `/start` пользователь сохраняется в БД. Повторный `/start`
не создаёт дубликат.

## Контекст
Пакет `internal/users` владеет доменом и хранилищем. Use case
`RegisterUser` идемпотентен.

## Действия
1. Миграция `0002_users_extend.up/down.sql`:
   ```sql
   ALTER TABLE users ADD COLUMN interface_language TEXT NOT NULL DEFAULT 'en';
   ALTER TABLE users ADD COLUMN telegram_username TEXT;
   ALTER TABLE users ADD COLUMN first_name TEXT;
   ALTER TABLE users ADD COLUMN updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP;
   ```
2. `internal/users/user.go`: структура `User`.
3. `internal/users/repository.go`: интерфейс
   ```go
   type Repository interface {
     ByTelegramID(ctx, tgID int64) (*User, error)
     Create(ctx, u *User) error
   }
   ```
   Реализация `sqliteRepo` в `internal/users/sqlite.go`.
4. `internal/users/usecase.go`: `RegisterUser(ctx, tgUser TelegramUser)
   (*User, bool, error)` — bool = `created`. Логика: lookup → если
   нет, insert с `interface_language` из `language_code` (нормализуем
   до ru/en/es, остальное → en).
5. Интегрировать в `/start`-handler: вызов `RegisterUser`, в логах
   `user_id`, `created`. Ответ — приветствие из i18n (на языке из
   профиля пользователя).

## DoD
- Первый `/start` от нового пользователя → запись в `users`.
- Второй `/start` → запись не дублируется, `created=false` в логах.
- Юнит-тест на `RegisterUser` с in-memory репо (или sqlite в tmpfile).
- `make build` зелёный, `make test` зелёный.

## Зависимости
Шаги 3, 4.
