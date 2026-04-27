# Шаг 9. Извлечение статьи по URL (4–5h)

## Цель
По URL получить заголовок и основной текст. Игнорировать навигацию,
рекламу, подвалы.

## Контекст
Библиотека: `github.com/go-shiori/go-readability` (port Mozilla
Readability). HTTP-клиент с таймаутом и лимитом размера тела.

## Действия
1. `internal/articles/extractor.go`: интерфейс
   ```go
   type Extractor interface {
     Extract(ctx, rawURL string) (Article, error)
   }
   type Article struct {
     URL, NormalizedURL, URLHash, Title, Content, Lang string
   }
   ```
2. `internal/articles/extractor_readability.go`: реализация на
   go-readability. HTTP-клиент с `Timeout: cfg.HTTPTimeoutSec * time
   .Second`, `http.MaxBytesReader` на body (`MAX_ARTICLE_SIZE_KB`).
3. Нормализация URL: убрать `utm_*`, `fbclid`, `gclid`, fragment
   (`#...`); привести host к lower-case; убрать trailing slash.
4. `URLHash` = `sha256(normalized_url)` в hex.
5. Ошибки различать: `ErrNetwork`, `ErrTooLarge`, `ErrNotArticle`
   (readability вернул пустой контент), `ErrPaywall` (эвристика —
   очень короткий контент + ключевые слова, опционально).
6. Тесты: golden-тест с локальными HTML-фикстурами в
   `internal/articles/testdata/` (Wikipedia, Habr, BBC сэмплы).

## DoD
- На 5 тестовых URL (вручную: Wikipedia, BBC, Habr, El País,
  NYT-non-paywall) — корректный заголовок и читаемый body.
- Юнит-тест на нормализацию URL покрывает utm/fragment/case.
- При запросе на 30MB-страницу — `ErrTooLarge`, не OOM.
- `make build` + `make test` зелёные.

## Зависимости
Шаг 1.
