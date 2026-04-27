# Шаг 7. Онбординг — выбор языка изучения и уровня (4–5h)

## Цель
После первого `/start` — мини-wizard: выбор языка изучения (ru/en/es)
и CEFR-уровня (A1–C2). Сохраняется в `user_languages`.

## Контекст
Начало Фазы 1 (Walking Skeleton). FSM состояний — in-memory map
`map[telegramUserID]onboardingState` под `sync.Mutex`. Простая
конечная машина: `awaiting_language` → `awaiting_level` → `done`.

## Действия
1. Миграция `0003_user_languages.up/down.sql`:
   ```sql
   CREATE TABLE user_languages (
     id INTEGER PRIMARY KEY AUTOINCREMENT,
     user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
     language_code TEXT NOT NULL,        -- ru|en|es
     cefr_level TEXT NOT NULL,           -- A1..C2
     is_active INTEGER NOT NULL DEFAULT 1,
     created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
     UNIQUE(user_id, language_code)
   );
   ```
2. `internal/session/onboarding.go`: FSM, `Start(userID)`, `Set...`,
   `State(userID)`. Чистка по таймауту (например, 30 минут idle).
3. `internal/users/languages.go`: репо `UserLanguageRepository` с
   методами `Set`, `Active(userID)`.
4. `internal/telegram/handlers/onboarding.go`: inline-кнопки для
   языков (RU/EN/ES) и уровней (A1, A2, B1, B2, C1, C2). Callback
   data: `onb:lang:ru`, `onb:level:b1` и т.д.
5. После `/start`: если у пользователя нет активного языка —
   запускаем wizard; если есть — обычное приветствие.
6. i18n-строки: `onb.choose_language`, `onb.choose_level`, `onb.done`.

## DoD
- Новый пользователь после `/start` видит выбор языка → выбора уровня
  → подтверждение.
- В `user_languages` появляется запись с `is_active=1`.
- Повторный `/start` приветствует, не запускает wizard заново.
- Незаконченный onboarding (закрыл клавиатуру) можно продолжить
  отправкой `/start` снова.
- `make build` + `make test` зелёные.

## Зависимости
Шаг 6.
