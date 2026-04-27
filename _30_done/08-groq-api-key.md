# Шаг 8. Подключение Groq API-ключа (5–6h)

## Цель
Пользователь вводит свой Groq API-ключ; он валидируется тестовым
запросом и сохраняется в БД зашифрованным.

## Контекст
Шифрование AES-256-GCM. Master-key — `ENCRYPTION_KEY` из `.env` (32
байта, base64). Потеря master-key = потеря всех ключей. Об этом
явно написать в README.

## Действия
1. Миграция `0004_user_api_keys.up/down.sql`:
   ```sql
   CREATE TABLE user_api_keys (
     id INTEGER PRIMARY KEY AUTOINCREMENT,
     user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
     provider TEXT NOT NULL,             -- 'groq'
     ciphertext BLOB NOT NULL,
     nonce BLOB NOT NULL,
     created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
     UNIQUE(user_id, provider)
   );
   ```
2. `internal/crypto/aesgcm.go`: `Encrypt(plain []byte) (cipher, nonce
   []byte, err)`, `Decrypt(cipher, nonce []byte) ([]byte, error)`.
   Master-key загружается из `cfg.EncryptionKey` (base64-decode).
3. `internal/llm/provider.go`: интерфейс
   ```go
   type Provider interface {
     ValidateAPIKey(ctx, key string) error
     // Analyze(...) — добавится в шаге 10/12
   }
   ```
4. `internal/llm/groq/client.go`: реализация `Provider` для Groq
   (OpenAI-совместимый endpoint `https://api.groq.com/openai/v1`).
   `ValidateAPIKey` делает простой `GET /models` или короткий
   completion. Различает сетевую ошибку, 401 (невалидный ключ),
   429 (нет квоты).
5. `internal/users/apikey_repo.go`: `Set(userID, provider, plain)` —
   шифрует и upsert; `Get(userID, provider)` — расшифровывает.
6. FSM-состояние `awaiting_api_key` (после онбординга, если ключа
   нет). `internal/telegram/handlers/apikey.go`:
   - `/setkey` (или кнопка) → бот просит прислать ключ;
   - получив текст, проверяет prefix `gsk_` (мягко, не блокирующе),
     зовёт `ValidateAPIKey`, при успехе — `Set`, при ошибке —
     понятное сообщение пользователю.
7. **Безопасность логов**: ключ НЕ должен попасть в логи. Логировать
   только факт успеха/провала и `user_id`. Удалить сообщение
   пользователя с ключом после сохранения (Telegram `DeleteMessage`).

## DoD
- Пользователь вводит ключ → бот делает реальный тестовый вызов →
  при успехе записывает в БД зашифрованным.
- Расшифровка корректно возвращает исходный ключ (юнит-тест на
  round-trip).
- Невалидный ключ → понятное сообщение, ничего не сохраняется.
- В `bot.log` нет ни одного байта ключа (grep по фрагменту = 0).
- Сообщение пользователя с ключом удаляется из чата.
- `make build` + `make test` зелёные.

## Зависимости
Шаг 7.

## Риск
Master-key должен быть сгенерирован при первом деплое и сохранён
надёжно (например, в защищённом backup). Потеря = невозможность
расшифровать ключи всех пользователей. Зафиксировать в README.
