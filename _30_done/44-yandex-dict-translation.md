# Task 44: Yandex Dictionary translation enrichment

## Goal

Replace LLM-generated `translation_native` with Yandex Dictionary API translations to eliminate hallucinated Russian words (e.g., "запяхающий" for "compelling"). LLM translation is kept as fallback when Yandex returns nothing or an error occurs.

## What to do

1. Create `internal/translation/` package with:
   - `Translator` interface (`translator.go`)
   - `YandexClient` HTTP client (`yandex.go`) — calls Yandex Dictionary REST API, returns top 3 translations joined with `", "`, empty string when not found
   - Tests using httptest (`yandex_test.go`)

2. Add `YandexDictAPIKey string` field (env `YANDEX_DICT_API_KEY`) to `internal/config/config.go`

3. In `internal/articles/usecase.go`:
   - Add `Translator translation.Translator` to `ServiceDeps` and `translator` to `Service`
   - After LLM call + safety check, before DB persist: enrich each word's `TranslationNative` via the translator; log warning + fall back on error; skip if translator is nil

4. In `cmd/bot/main.go`: construct `translation.NewYandex(cfg.YandexDictAPIKey)` when key is set, inject into `articles.ServiceDeps`

## Definition of done

- [ ] `make build` green
- [ ] `make test` green
- [ ] Bot works without `YANDEX_DICT_API_KEY` set (LLM fallback)
- [ ] Bot enriches translations when `YANDEX_DICT_API_KEY` is set
