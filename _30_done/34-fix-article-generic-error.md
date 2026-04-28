# 34 — fix-article-generic-error

## Цель

Любая отправленная статья отдаёт «Что-то пошло не так. Попробуйте позже.» вместо
карточки. Корень — модель возвращает валидный по схеме JSON, но с пустыми
строками в `adapted_versions.{lower,current,higher}`. Дальше:
- `articles.adaptedFromLLM` фильтрует пустые → в БД сохраняется `{}`;
- рендер карточки регенерирует `current` через `articles.Adapt`;
- `pickAdaptSource` находит пусто → `articles.ErrNoSourceText`;
- `articleErrorMessageID` его не знает → `default` → `error.generic`.

Прежний симптом «Модель вернула странный ответ» приходил из `ErrSchemaInvalid`,
когда модель ломала схему. Сейчас она проходит схему, но содержит пустоту —
поэтому пользователь видит немое generic-сообщение. Параллельно — голый
`errors.New("groq: empty choices")` в groq.client тоже падает в `default`.

## Что сделать

1. **Схема `internal/llm/schema/analysis.json`**: добавить `"minLength": 1` на
   `adapted_versions.current`. `lower` и `higher` оставить как есть — они лениво
   регенерируются. `current` — обязательный минимум, без него карточку
   рендерить нечем. Эффект: пустой `current` теперь будет отклонён схемой,
   `groq.Analyze` сделает один retry с фидбеком; если и второй раз пусто →
   `llm.ErrSchemaInvalid` → пользователь увидит «Модель вернула странный
   ответ. Попробуй ещё раз через минуту.» (диагностируемое сообщение, а не
   немое).

2. **Fallback в `internal/articles/usecase.go::Adapt`**: когда
   `pickAdaptSource` вернул пустой `text`, использовать
   `article.SummaryTarget` как `sourceText` и `article.CEFRDetected` как
   `sourceCEFR`. Это мягкая миграция для уже сохранённых записей с пустым
   `adapted_versions` — без неё они навсегда нечитаемы. Новые записи после
   фикса 1 не должны попадать в этот fallback.

3. **Маппинг ошибок в `internal/telegram/handlers/url.go::articleErrorMessageID`**:
   добавить ветку `errors.Is(err, articles.ErrNoSourceText) → "article.err.llm_format"`.
   По смыслу пустой источник — деградировавший ответ модели, та же категория
   что `ErrSchemaInvalid`. Используем существующий i18n-ключ.

4. **Wrap голой ошибки в `internal/llm/groq/client.go:138`**: `errors.New("groq:
   empty choices")` → `fmt.Errorf("%w: empty choices", llm.ErrUnavailable)`.
   Аудит остального `groq/{client,analyze,adapt}.go` на подобные голые
   ошибки — должны быть в `llm.Err{InvalidAPIKey,RateLimited,Unavailable,SchemaInvalid}`.

5. **Fixtures**: проверить, что `internal/llm/mock/fixtures/analyze_clean.json`,
   `analyze_flagged.json` и `internal/llm/testdata/valid.json` имеют непустой
   `adapted_versions.current` (если уже непустой — ничего не делать).

## Definition of Done

- [ ] `make build` зелёный.
- [ ] `make test` зелёный (включая существующие схема-тесты).
- [ ] Ручной прогон: `./bin/tg-linguine` запускается, отправка эталонной
      ссылки даёт карточку статьи (без «Что-то пошло не так»). Проверить два
      сценария: новая статья и cache hit на ту же URL.
- [ ] Один git-коммит `step 34: fix-article-generic-error`.
