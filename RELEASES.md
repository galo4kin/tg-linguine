# Releases

## 08 — groq-api-key
Подключили хранение Groq API-ключа: миграция `0004_user_api_keys`,
AES-256-GCM в `internal/crypto`, `llm.Provider` + Groq-клиент с
классификацией ошибок (401/429/сеть), `users.APIKeyRepository` с
upsert через шифрование, команда `/setkey`, удаление сообщения с
ключом из чата и заметка про мастер-ключ в README/`.env.example`.
Логи не содержат значение ключа — только `user_id` и причину.

## 07 — onboarding
Добавлен мини-wizard выбора языка изучения и CEFR-уровня: миграция
`0003_user_languages`, in-memory FSM в `internal/session` с TTL 30 минут,
SQLite-репо `UserLanguageRepository.Set/Active` и inline-кнопки с
callback'ами `onb:lang:*` / `onb:level:*`. `/start` запускает wizard, если
у пользователя ещё нет активного языка, и здоровается обычным образом, если
язык уже выбран; незавершённый шаг можно продолжить повторным `/start`.

## 06 — user-repo
Появился пакет `internal/users` с доменной структурой `User`, SQLite-репозиторием
и идемпотентным use case `RegisterUser`; миграция `0002_users_extend` добавляет
`interface_language`, `telegram_username`, `first_name`, `updated_at`, а
`/start` теперь регистрирует пользователя при первом обращении и логирует флаг
`created`.

## 05.5 — refactor-i18n-bundle
Заменили загрузку локалей через `init()` с `panic` на явный конструктор
`i18n.NewBundle() (*i18n.Bundle, error)`; bundle теперь прокидывается из
`main.go` в `telegram.New`, а ошибка чтения YAML возвращается наружу.
