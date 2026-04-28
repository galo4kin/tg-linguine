# Code review: steps 36–42

Триггер обзора — шаг 40 (multiple of 5). Просмотрены коммиты:
`55e863a` (36), `41baf68` (37), `8b0b543` (38), `b32bd64` (39),
`8c9198d` (40), `96dd811` (41), `c2e1e20` (42).

## Что хорошо

- Архитектура «парковки» длинной статьи (`PendingStore` +
  `AnalyzeResult{Article|LongPending}` + callback `lng:<id>:<v>`) —
  чистое разделение слоёв: usecase ничего не знает про i18n, а
  Telegram-handler ничего не знает про TTL/store. `NoticeRenderer` —
  правильный способ протащить локализацию через сервис.
- `parseRateLimitRetryAfter` (header → body fallback, clamp до 60s) +
  тесты на восемь кейсов в `retry_after_test.go` — аккуратно.
- `snapshotErrorBody` ловит реальное сообщение Groq в одно место и
  попадает и в лог, и в обёрнутую ошибку — ровно тот объём
  диагностики, который нужен.
- Прогрессивное движение по ошибкам (37→38→39→40→41→42) хорошо
  задокументировано в коммит-сообщениях: каждый следующий шаг
  опирается на лог-данные предыдущего, а не на догадки.
- Обновлённый `system.txt` со встроенной OUTPUT SCHEMA + маркеры
  `<<<INPUT>>> / <<<END INPUT>>>` в `user.tmpl` — корректное решение
  обнаруженной проблемы (модель путала input-поля с output-полями).

## Что можно улучшить

### 1. `AnalyzedArticle.Article *Article` — двойное `.Article.Article`

`internal/articles/usecase.go:117-121` объявляет
```go
type AnalyzedArticle struct {
    Article *Article
    Words   []dictionary.DictionaryWord
    Notice  string
}
```
В сочетании с `AnalyzeResult.Article *AnalyzedArticle` это даёт
цепочки `result.Article.Article.ID`, что и видно в тестах
(`internal/articles/usecase_test.go:120, 123, 375, 376`). Имя
`Article` перегружено на двух уровнях.

**Действие:** переименовать поле `AnalyzedArticle.Article` →
`AnalyzedArticle.Stored` (или `Record`). Цепочки превращаются в
`result.Article.Stored.ID`, что читается. Плюс тесты в `long_test.go`
и `usecase_test.go`.

### 2. `chat()` и `chatPlainText()` — почти одинаковые retry-петли

`internal/llm/groq/analyze.go:111-151` (chat) и
`internal/llm/groq/summarize.go:53-89` (chatPlainText) отличаются
только: тип возврата (`[]byte` vs `string`), форма chatRequest
(json_object vs без response_format), лог-префикс
(`groq.chat` vs `groq.summarize`). Сама петля — атрибут (попытка,
buffer, clamp до maxRetryAfter, ctx.Done) — продублирована.

Также продублирован clamp `wait > maxRetryAfter` (analyze.go:132-134,
summarize.go:70-72), хотя `parseRateLimitRetryAfter` уже клампит
через `clampRetryAfter`. Прибавление `retryAfterBuffer` может снова
выкатить сумму за предел — строго говоря, нужно. Но дублирование
само по себе осталось.

**Действие:** вытащить общий retry-loop в `client.go`, например
`func (c *Client) withRateLimitRetry(ctx, callOnce func() (T, time.Duration, error), logPrefix string) (T, error)`
с дженериком, или просто пробросить `*chatResponse` и сделать общий
helper. Заодно single-source clamp.

### 3. `runAnalysis` повторно достаёт user/key, уже проверенные в `AnalyzeArticle`

`internal/articles/usecase.go:198-207` валидирует наличие
user/`ProviderGroq`-key. Потом `AnalyzeArticle` (l. 284) вызывает
`runAnalysis`, который на l. 421-428 снова делает
`s.users.ByID()` и `s.keys.Get()`. Удвоенный round-trip к БД на
горячем пути.

`AnalyzeExtracted` тоже частично повторяется: для `ModeSummarize`
ключ достаётся в свитче (l. 364-370), а потом снова в `runAnalysis`.

**Действие:** либо убрать presence-проверки из `AnalyzeArticle` —
`runAnalysis` всё равно их делает, нужна только маппинг ошибки
`users.ErrNotFound → ErrNoActiveLanguage / ErrNoAPIKey`; либо принять
`*users.User` и `key string` в `runAnalysis` как аргументы и
обязать всех вызывающих их подготовить. Второе — чище и согласуется с
fact-of-life "у нас уже всё есть в верхнем слое".

### 4. `summarizeInputBudget` живёт в `articles/usecase.go`

`internal/articles/usecase.go:36-39`. Константа описывает лимит
конкретно Groq-free-tier TPM, относится к лимитам LLM, но лежит в
`articles`. Та же группа констант (TPM-границы) уже размазана:
`DefaultMaxTokensPerArticle` тоже в articles, `analyzeMaxCompletionTokens`
в groq, `MaxTokens` поле в `chatRequest` — в groq. Связь между ними
держится только в комментариях (5K input + 4K output + ~1K system ≤
12K TPM).

**Действие:** свести Groq-TPM-budget константы в одно место (например
`internal/llm/groq/budget.go`), оставив `articles.Service.maxTokens`
параметром, который инициализируется снаружи (`config` уже это
делает). `summarizeInputBudget` тогда тоже там, и комментарий о
связи 7500 + 3500 + 500 ≤ 12K читается рядом с другими цифрами.

### 5. Schema-retry в `analyze.go` логируется, в `adapt.go` — нет

`internal/llm/groq/analyze.go:78-89` добавляет `Warn` со snippet/len
при schema-retry. `internal/llm/groq/adapt.go:34-46` — тот же
паттерн (assistant message, retry user message), но без warn. Для
будущих "почему опять JSON не пройдёт валидацию" в Adapt-пути нет
diagnostics.

Само по себе это просто асимметрия, но обе функции — почти полная
копия друг друга (Analyze: 78-102; Adapt: 34-47), различия — имя
схемы валидации и тип ответа.

**Действие:** вынести schema-retry-обвязку в helper в `groq` пакете,
например
```go
func (c *Client) chatJSONWithSchemaRetry(ctx, key, model, messages, validate func([]byte) error) ([]byte, error)
```
Asymmetry лога снимается тем, что log живёт внутри helper, и
Adapt получает diagnostics бесплатно.

### 6. Плотность комментариев в новом коде

CLAUDE.md явно говорит "Default to writing no comments". В этом
прогоне добавлено ~174 строки комментариев в основные файлы. Большая
часть — оправданная: объясняют WHY (free-tier TPM, почему clamp 60s,
почему +500 slack). Но есть и описательные:

- `internal/articles/usecase.go:97-99` — `Stage notifies the caller of progress; used by the Telegram handler to edit the status message between long-running steps.` Тип называется `Stage`, рядом константы `StageFetching/StageAnalyzing/StagePersisting`. WHAT-comment.
- `internal/articles/usecase.go:75-77` — `ApproxWordCount is purely for the user-facing rejection message …`. Имя метода уже даёт WHAT; "purely for" — лишнее, тем более что после step 36 функция используется не только для rejection, а и для banner, и для prompt-а. Комментарий устаревает.
- `internal/articles/pending_store.go:84-85` — `Size reports the number of currently parked items. Test-only helper.` Описывает API, который и так очевиден; тег "test-only" не enforced.
- `internal/llm/groq/client.go:73-78` — `defaultBackoff is consulted on retryable failures (5xx and transport errors). The slice length is the number of retries — 2 attempts after the first failure, with the documented 1s, 3s waits.` — длинное описание схемы, которая прямо рядом в коде; конкретные значения и есть документация.

Это не блокер, но если делать вычистку — поправить.

### 7. `pending_store.go` — `now func() time.Time`

`internal/articles/pending_store.go:31, 43`. Паттерн
консистентен с `session/onboarding.go`, `session/apikey.go`,
`session/study.go`, `telegram/ratelimit.go` — всё ок. Замечаний нет,
проверял по запросу.

### 8. `gcLocked` крутит O(n) проход по всей мапе на каждом Put/Take/Size

`internal/articles/pending_store.go:92-98`. На бот с одним
single-instance состоянием и TTL=10мин это не проблема, но если
поставить DefaultPendingTTL побольше, число записей растёт. Не
критично — указываю как фактаж, не баг.

### 9. `parseLongArticleCallback` — magic strings `"t"` / `"s"`

`internal/telegram/handlers/long_article.go:131-135`. Маппинг
варианта в callback на `LongAnalysisMode` сидит здесь. Если
варианты добавятся, нужно править и payload, и switch. Сейчас
читается, держать в голове два места не страшно. Замечание мелкое.

### 10. `system.txt` — 42 строки с встроенной схемой

CLAUDE.md ничего не говорит о промптах. Файл вырос с описания роли
до описания + literal output schema. Альтернатива — отдельный
`schema.txt` и склейка при `SystemPrompt()`. Но это синтетический
рефактор, ради чистоты ради чистоты. Решение оставить как есть —
читаемо, схема видна целиком.

## Предлагаемые refactor-задачи

Очередь `_10_todo/` сейчас пустая (только `00-schema.md`-якорь),
поэтому NEXT = 43, и refactor-файлы по конвенции CLAUDE.md
именуются `42.M-...` (M от 5). По убыванию ценности:

- **`42.5-refactor-analyzed-article-naming.md`** — переименовать
  `AnalyzedArticle.Article` в `AnalyzedArticle.Stored` (или
  `.Record`); прогнать тесты в `articles/usecase_test.go`,
  `articles/long_test.go`, и в `telegram/handlers/long_article.go`
  + `url.go`. Тривиальный sed + сборка. Закрывает finding §1.
- **`42.6-refactor-groq-rate-limit-loop.md`** — вытащить общий
  retry-loop в `groq/client.go`, чтобы `chat`/`chatPlainText` стали
  тонкими обёртками над `withRateLimitRetry`. Заодно single-source
  clamp `retryAfterBuffer + maxRetryAfter`. Закрывает finding §2.
- **`42.7-refactor-runanalysis-no-double-fetch.md`** — убрать
  presence-проверки из `AnalyzeArticle` (или принимать user/key
  параметром в `runAnalysis`); привести `AnalyzeExtracted` под ту же
  модель. Закрывает finding §3.
- **`42.8-refactor-groq-tpm-constants-colocate.md`** — собрать
  TPM-budget константы (`DefaultMaxTokensPerArticle`,
  `summarizeInputBudget`, `analyzeMaxCompletionTokens`,
  `maxRetryAfter`, `retryAfterBuffer`, `maxRateLimitAttempts`) в
  одно место с общим комментарием о 12K free-tier TPM и
  взаимосвязях. Закрывает finding §4.
- **`42.9-refactor-groq-schema-retry-helper.md`** — вытащить
  обвязку schema-retry в общий helper для `Analyze` и `Adapt`,
  логирование snippet/len становится единым. Закрывает finding §5.

§§ 6, 8, 9, 10 — мелкие наблюдения, отдельных тасок не стоят;
имеет смысл прихватить §6 в один из refactor-задачников выше, если
по дороге попадётся подходящий комментарий.
