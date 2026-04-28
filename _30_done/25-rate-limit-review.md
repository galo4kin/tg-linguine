# Code review для шага 25 (rate-limit)

Период: коммиты от `087dcb6` (review 20) по `2ab6ba3` (step 25).
Шаги в окне: 21 (my-words), 22 (flashcards), 23 (categories),
24 (delete-me), 25 (rate-limit).

## Что в порядке

- Package-by-feature соблюдён: `internal/telegram/ratelimit.go`
  лежит в транспортном слое, доменные пакеты чистые.
- FSM-сессии (`onboarding`, `study`, `apikey`) сосредоточены в
  `internal/session/` и аккуратно вычищаются в `delete.go`.
- Handlers получают зависимости через конструктор, side-effects не
  расползаются по пакету.
- Юнит-тесты на новые компоненты (study FSM, ratelimit, delete repo)
  покрывают и happy-path, и граничные случаи.

## Дубликат `EstimateTokens`

Сейчас одна и та же эвристика (`runes/4`) описана в двух местах:

- `internal/llm/analysis.go:109` — `llm.EstimateTokens` (старая,
  используется только в тестах LLM-пакета);
- `internal/articles/usecase.go:58` — `articles.EstimateTokens`
  (используется в `AnalyzeArticle` для отсечения длинных статей).

Это leaky-abstraction-«классика»: одна и та же утилита живёт в двух
пакетах с почти идентичной реализацией и расходящимся форматированием
(`(n+3)/4` vs ручной round-up). Стоит привести к одной функции — либо
выделить в `internal/textutil`, либо оставить в `llm` (формально это
оценка токенов LLM-пейлоада) и удалить копию из `articles`. Рефакторинг
маленький, но если оставить — третий раз обязательно появится.

## `apikey waiter` ключуется по Telegram ID, а study FSM по DB ID

`session.APIKeyWaiter` и `session.Onboarding` оперируют
`telegram_user_id`, а `session.Study` — внутренним `users.ID`. Сейчас
это работает, но в `delete.go` пришлось вызывать `End` и `Disarm`
с разными ID для одного и того же пользователя (см.
`telegram/handlers/delete.go:resetFSM`). Если в будущем появится ещё
один FSM, это станет источником багов. Было бы правильнее унифицировать
ключ (выбрать один) — но это не блокер на сейчас.

## i18n-ключи: ручная синхронизация

`en.yaml`, `ru.yaml`, `es.yaml` сейчас держатся «на честном слове» —
ничто не валидирует, что после добавления ключа в `en.yaml` он
появился и в двух других локалях. В шаге 28 как раз планируется
«i18n: для каждого ключа — наличие во всех трёх локалях». Так что
этот пункт уже взят в работу, отдельный refactor-шаг не нужен.

## Незначительное

- `internal/articles/usecase.go` подрос (≈340 строк); пока читаемо,
  но если добавим ещё один gate — стоит вынести проверки (cache,
  blocklist, token budget) в helper-функции. Не сейчас.
- В `_settings.go` остался уже неиспользуемый i18n-ключ
  `settings.delete.placeholder` — удалён в шаге 24, повторно
  чистить нечего.

## Итог

В работу выносится один refactor: устранить дубликат
`EstimateTokens`. Файл `_10_todo/27.5-refactor-estimate-tokens.md`
будет создан, чтобы следующий прогон взял его до step 28.
