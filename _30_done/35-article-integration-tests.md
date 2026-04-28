# 35 — article-integration-tests

## Цель

Добавить интеграционные тесты, которые ловили бы регрессии маппинга ошибок и
дрейф контракта Groq-модели. До этого таких тестов не было: текущая ошибка
«Что-то пошло не так» (см. шаг 34) пролетела ревью именно потому, что путь
URL-handler → article-pipeline → user message не покрыт сквозным тестом.

## Что сделать

### 1. URL-handler: интеграционный тест маппинга ошибок

Новый файл `internal/telegram/handlers/url_integration_test.go`. Поднимает:
- in-memory SQLite с миграциями (паттерн из `internal/articles/usecase_test.go`),
- `articles.Service` с `mock.Provider` (`internal/llm/mock`) и стабовым
  extractor'ом, возвращающим эталонную статью из fixture,
- мок Telegram API через `httptest.Server` + `bot.WithServerURL` (если опция
  есть; иначе — тонкий messenger-интерфейс в URLHandler), перехватывающий
  `sendMessage` / `editMessageText`.

Кейсы (table-driven):
| AnalyzeErr / setup                                       | Ожидаемый i18n key       |
| -------------------------------------------------------- | ------------------------ |
| nil (happy)                                              | карточка статьи          |
| `llm.ErrSchemaInvalid`                                   | `article.err.llm_format` |
| `llm.ErrUnavailable`                                     | `apikey.unavailable`     |
| `llm.ErrRateLimited`                                     | `apikey.rate_limited`    |
| `llm.ErrInvalidAPIKey`                                   | `apikey.invalid`         |
| `articles.ErrNoSourceText` (Adapt fail на cache hit)     | `article.err.llm_format` |
| сырая `errors.New("boom")`                               | `error.generic` (канарейка) |

Тест читает текст последнего перехваченного сообщения и сравнивает с
`tgi18n.T(loc, msgID, nil)` для русской локали.

### 2. Reference-article test против реального `groq.Client`

Новый файл `internal/llm/groq/analyze_reference_test.go`. Поднимает
`httptest.Server`, отдающий заранее записанный JSON Groq на
`POST /chat/completions`. Конструирует `groq.New(WithBaseURL(server.URL),
WithBackoff(nil))`. Вызывает `Analyze(ctx, "test-key", req)` с реалистичным
`AnalyzeRequest`.

Fixtures `internal/llm/groq/testdata/`:
- `reference_request_article.txt` — эталонная статья (~3000 знаков, EN).
- `reference_response_ok.json` — Groq envelope `{"choices":[{"message":{"content":"<inner>"}}]}`,
  inner — валидный по `analysis.json` ответ.
- `reference_response_empty_current.json` — inner с пустым
  `adapted_versions.current` (фиксирует фикс 34.1).
- `reference_response_bad_json.json` — битый inner-JSON.
- `reference_response_empty_choices.json` — envelope без `choices`
  (фиксирует фикс 34.4).

Кейсы (table-driven):
| Server response sequence                                                | Ожидание                              |
| ----------------------------------------------------------------------- | ------------------------------------- |
| `reference_response_ok`                                                 | success, парсится в `AnalyzeResponse` |
| `reference_response_bad_json`, `reference_response_ok`                  | success после ретрая                   |
| `reference_response_bad_json`, `reference_response_bad_json`            | `llm.ErrSchemaInvalid`                |
| `reference_response_empty_current`, `reference_response_empty_current`  | `llm.ErrSchemaInvalid`                |
| `reference_response_empty_choices`                                      | `llm.ErrUnavailable`                  |
| HTTP 401                                                                | `llm.ErrInvalidAPIKey`                |
| HTTP 429                                                                | `llm.ErrRateLimited`                  |
| HTTP 503                                                                | `llm.ErrUnavailable` (после ретраев)  |

### 3. Live smoke test (опциональный, по env-флагу)

Новый файл `internal/llm/groq/analyze_live_test.go`. `t.Skip` если
`GROQ_LIVE_TEST != "1"`. При активации:
- ключ из `GROQ_API_KEY`,
- грузит `testdata/reference_request_article.txt` как `ArticleText`,
- вызывает реальный `groq.New().Analyze(...)` с CEFR=B1, target=en, native=ru,
- проверяет: результат проходит схему, `adapted_versions.current` непустой,
  `len(words) > 0`.

В CI не запускается. Вручную:
`GROQ_LIVE_TEST=1 GROQ_API_KEY=... go test ./internal/llm/groq/ -run Live`.

## Definition of Done

- [ ] `make build` зелёный.
- [ ] `make test` зелёный, новые тесты проходят.
- [ ] `go test ./internal/telegram/handlers/... -run URL` — все интеграционные
      кейсы зелёные.
- [ ] `go test ./internal/llm/groq/... -run Reference` — все fixture-кейсы
      зелёные.
- [ ] `GROQ_LIVE_TEST=1 GROQ_API_KEY=... go test ./internal/llm/groq/... -run Live`
      прогнан вручную хотя бы раз, факт зафиксирован.
- [ ] Один git-коммит `step 35: article-integration-tests`.
