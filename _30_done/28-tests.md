# Шаг 28. Тесты критичных сценариев (5–7h)

## Цель
Coverage домена ≥70%. Прогон `make test` — зелёный.

## Действия
1. **Unit**: доменные сервисы с mock-репо (генерация моков —
   `gomock` или ручные стабы).
   - `RegisterUser` (шаг 6).
   - `AnalyzeArticle` happy path и ветки (нет ключа, длинная статья,
     paywall, LLM-ошибка, cache hit).
   - Flashcard FSM (mastered после 3, reset на ошибку).
   - Token-bucket rate limit.
2. **Integration**: in-memory sqlite (или tmpfile + миграции).
   Все репо: round-trip create/read/delete.
3. **Mock LLM-провайдер** в `internal/llm/mock` с фикстурами JSON.
4. **i18n**: для каждого ключа — наличие во всех трёх локалях
   (тест перебирает ключи в `en.yaml` и проверяет `ru`/`es`).
5. **Crypto**: round-trip Encrypt → Decrypt.
6. Замер coverage: `go test -cover ./...` — отчёт по пакетам, цель
   ≥70% по `internal/users`, `internal/articles`,
   `internal/dictionary`, `internal/session`, `internal/crypto`.

## DoD
- `make test` зелёный.
- `go test -cover ./internal/...` ≥70% по доменным пакетам.
- В CI/локально тесты завершаются <60s.
- `make build` зелёный.

## Зависимости
Все предыдущие.
