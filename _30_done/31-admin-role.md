# Шаг 31. Админ-роль: конфигурация и гейт (1–2h)

## Цель
Ввести понятие админа и его конфигурацию — без UX-команд. Это
фундамент для шагов 32 (админ-команды) и 33 (стартовый пинг).

## Контекст
Аналог в соседнем проекте `~/Projects/tg-boltun`:
- `ADMIN_USER_ID` читается в `main.go:84-86`, хранится в
  `adminUserID int64`.
- Гейт по `userID != adminUserID` в `main.go:495`.

В нашем проекте конфиг живёт в `internal/config/`, Telegram-слой —
в `internal/telegram/`. Хелпер `IsAdmin` логично положить рядом с
остальными middleware/utility в `internal/telegram/`.

## Действия
1. `internal/config/config.go` — добавить поле `AdminUserID int64`
   с тегом `env:"ADMIN_USER_ID"`. Опциональное, default `0` =
   админа нет, админ-функции выключены.
2. `.env.example` — добавить:
   ```
   # Telegram user id админа (получить через @userinfobot).
   # Пусто = админ-функции выключены. Пример (текущий владелец):
   ADMIN_USER_ID=1995215
   ```
3. `internal/telegram/` — публичный хелпер
   `IsAdmin(cfg, userID int64) bool` (или метод на bot/middleware,
   как удобнее по структуре пакета). Поведение: `cfg.AdminUserID != 0
   && userID == cfg.AdminUserID`.
4. README — раздел «Admin»: как узнать свой Telegram ID
   (через `@userinfobot`; для текущего владельца это `1995215`), что
   переменная опциональна, что без неё все админ-команды и стартовый
   пинг отключены.
5. На старте `cmd/bot/main.go` — структурный лог:
   `slog.Info("admin configured", "user_id", cfg.AdminUserID)` если
   задан, иначе `slog.Info("admin disabled")`.

## DoD
- Бинарник стартует и при наличии, и при отсутствии
  `ADMIN_USER_ID`.
- `IsAdmin` возвращает `false` для всех, если `AdminUserID == 0`.
- В логе на старте видно состояние админ-конфига.
- README покрывает раздел «Admin».
- `make build` + `make test` зелёные.

## Зависимости
Шаг 30 (финальный деплой v1.0). Эти задачи — мини-фаза v1.1.
