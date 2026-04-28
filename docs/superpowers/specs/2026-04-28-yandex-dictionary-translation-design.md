# Yandex Dictionary Translation Integration

## Problem

The LLM (Groq) generates `translation_native` as part of the article analysis prompt. It occasionally hallucinates non-existent words (e.g., "запяхающий" for "compelling"). A dedicated dictionary API produces more reliable, lexicographically correct translations.

## Solution

Integrate Yandex Dictionary API to replace `translation_native` after the LLM call. LLM translation is kept as fallback when Yandex returns nothing.

## Architecture

```
LLM.Analyze() → []AnalyzedWord
    ↓ per word (lemma)
YandexDict.Translate(lemma, "en-ru") → "убедительный, неотразимый"
    ↓ success → overwrite TranslationNative
    ↓ error/empty → keep LLM translation
→ store to DB
```

## New Files

- `internal/translation/translator.go` — `Translator` interface
- `internal/translation/yandex.go` — HTTP client for Yandex Dictionary API (`https://dictionary.yandex.net/api/v1/dicservice.json/lookup`)

## Modified Files

- `internal/config/config.go` — add `YandexDictAPIKey string` (`env:"YANDEX_DICT_API_KEY"`)
- `internal/articles/usecase.go` — add `translator Translator` field; enrich words after LLM call
- `cmd/bot/main.go` — construct Yandex client and inject into UseCase

## Key Decisions

- Lang pair is `targetLanguage + "-" + user.InterfaceLanguage` (e.g., `en-ru`)
- Top 2–3 translations joined with `", "` (e.g., `"убедительный, неотразимый"`)
- If `YANDEX_DICT_API_KEY` is empty → skip enrichment, behave as before
- No changes to DB schema — `translation_native` field already exists

## Verification

1. Set `YANDEX_DICT_API_KEY` in `.env`
2. Submit an English article via the bot
3. Verify word cards show correct Russian translations (not hallucinated words)
4. Unset the key → verify bot still works with LLM translations
