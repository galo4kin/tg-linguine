# Review 30 (шаги 26–30 + 27.5)

Окно: всё, что закрыто после `review 25` — `26-long-articles`,
`27-content-safety`, `27.5-refactor-estimate-tokens`, `28-tests`,
`29-hardening`, `30-readme-sync`.

Прогон `make build` + `make test` зелёный. `go vet ./...` тоже
молчит. Архитектурно package-by-feature не нарушен: появился
`internal/llm/mock` — это всё ещё `llm/`, и он не тащит за собой
ничего лишнего (`embed` + `internal/llm` для схемы).

## Найденные refactor-кандидаты

### 1. Дублирование stub-LLM: `articles.stubLLM` vs `mock.Provider`

После шага 28 в дереве появилось два независимых стенда `llm.Provider`:

- приватный `stubLLM` в `internal/articles/usecase_test.go:29-51` —
  старый, ручной, с полями `calls`, `lastRequest`, `lastAdaptRequest`;
- exported `mock.Provider` в `internal/llm/mock/mock.go:25-117` —
  новый, fixture-driven, с тем же набором возможностей плюс
  валидация ответов через реальную JSON-схему.

`articles_test` всё ещё везде использует `stubLLM` (см.
`articles/usecase_test.go:128, 180, 203, 229, 265, 370`,
`articles/long_test.go:87, 157, 196`). По сути это два параллельных
способа делать одно и то же — классический случай "ввели
канонический инструмент, но не мигрировали потребителей".

→ Заведена refactor-задача `30.5-refactor-stub-llm-vs-mock-provider.md`.

### 2. Дубль определения языка пользователя в bot.go

Шаг 29 ввёл хелпер `langFromUpdate` в `internal/telegram/bot.go:194`
для panic-recovery middleware. Но `i18nMiddleware` чуть ниже
(`bot.go:218-229`) повторяет ту же логику in-line — switch на
`update.Message.From.LanguageCode` / `update.CallbackQuery.From.LanguageCode`.
Две копии одного решения, и если завтра появится новый источник
(например, business-message), баг проявится в одной, а не в обеих.

→ Заведена refactor-задача `30.6-refactor-i18n-middleware-dry.md`.

### 3. `ApproxWordCount`: комментарий vs реализация

`internal/articles/usecase.go:52-72`. Комментарий обещает
`strings.Fields`, реализация — ручной char-by-char цикл, который
разбивает только по ASCII whitespace (` `, `\t`, `\n`, `\r`).
Поведение под `\v`/`\f`/U+00A0 тихо отличается от того, что обещает
комментарий. Тесты в `long_test.go:34-48` это не ловят, потому что
все кейсы в них — ASCII.

Это локальный, безопасный фикс на месте: либо переписать на
`len(strings.Fields(text))`, либо обновить комментарий. Не выношу в
отдельную задачу — следующий, кто будет рядом, починит за минуту.

## Что специально НЕ переделывал

- Дубль retry-on-schema-validation между `groq.Analyze` и `groq.Adapt`
  существует с шагов 10 и 18, не введён в текущем окне — оставляем.
- `chatIDFromUpdate` потенциально полезен handlers/, но они уже
  знают, какой тип update'а пришёл, и обращаются к нужному полю
  напрямую. Не считаем дубликатом.
- `internal/llm/mock` экспортирует `LoadAnalyze`/`LoadAdapt` отдельно
  от `New()`. На первый взгляд лишний публичный API, но fixture
  loader полезен для тестов, которые хотят кастомизировать ответ
  до создания Provider — оставляем.
