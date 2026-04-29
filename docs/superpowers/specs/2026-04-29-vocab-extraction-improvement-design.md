# Vocab Extraction Improvement for Long Articles

## Context

When a user submits a long article (e.g., 7694 words), the bot either truncates it to ~1250 words or summarizes it to ~625 words before sending to the LLM. The LLM only sees the compressed text, so it extracts just 5 new words instead of the 15-20+ that a B2 learner should get from such a rich article. Vocabulary from the remaining 84% of the article is completely lost.

The goal: extract vocabulary from the **full article text** while keeping the existing summary/adapted flow on compressed text.

## Design

### New `ExtractVocab` LLM method

A new method on the `Provider` interface, following the pattern of `Analyze`, `Adapt`, `Summarize`:

```go
ExtractVocab(ctx context.Context, key string, req ExtractVocabRequest) (ExtractVocabResponse, error)
```

**Why a separate method** (not a flag on Analyze):
- Response schema is different — just a `words` array, no summary/adapted/safety_flags
- Lighter system prompt saves tokens (~500 vs ~1000)
- Smaller output cap (2000 vs 4000 tokens) — matters for 12K TPM budget

**Request fields**: `TargetLanguage`, `NativeLanguage`, `CEFR`, `KnownWords`, `AlreadyFoundLemmas`, `ArticleText`, `VocabTarget`.

**Response**: `{ "words": [<same AnalyzedWord shape as Analyze>] }`

### Chunking strategy

For long articles, the full text is split into **up to 2 chunks** of ~5000 tokens each, cut at paragraph boundaries (reusing `TruncateAtParagraph`):
- Chunk 1: first ~5000 tokens of the original text
- Chunk 2: next ~5000 tokens (remainder truncated at paragraph boundary)
- Text beyond ~10K tokens is dropped

Chunks cover the full original text from the beginning, including portions already seen by the main Analyze call (which processed a summarized or truncated version). Deduplication by lemma handles overlap — re-scanning the original catches words that the summarizer dropped or the truncation cut off.

Each chunk is sent to `ExtractVocab` **sequentially** (Groq rate limits). Between chunks, newly found lemmas are added to the exclusion list.

### Word merging

Words from all sources are merged:
1. Primary words from main `Analyze` response come first
2. Extra words from vocab chunks are appended, deduplicated by lowercased lemma
3. Total capped at `VOCAB_TARGET_WORDS`

If the vocab extraction fails (rate limit exhausted, API error), the main analysis still succeeds with just the primary words — vocab extraction is best-effort.

### Configuration

New env variable in `config.go`:
```
VOCAB_TARGET_WORDS=20  (default)
```

Passed through `ServiceDeps` → `Service` → prompts. Controls the target word count per chunk and the final merge cap.

### Adapted versions minimum length

Add prompt instruction to `system.txt`: when adapting an original article (not a pre-summary), `adapted_versions.current` should be at least 500 words. Only fall below 500 if the original article is shorter.

This is a soft constraint via prompt instruction, not code validation — avoids expensive retry loops.

### Threading full text through the pipeline

`runAnalysis` gets a new `fullText string` parameter:
- `AnalyzeExtracted` passes `extracted.Content` (original from pending store) as `fullText`
- `AnalyzeArticle` (short articles) passes `""` — no extra vocab pass needed
- When `fullText != "" && fullText != extracted.Content`, the extra vocab pass runs

### Progress reporting

New `StageExtractingVocab` stage between `StageAnalyzing` and `StagePersisting`. The Telegram handler shows "Извлечение словаря..." so the user knows why it's taking longer.

### Rate limit impact

| Call | Input tokens | Output tokens | System | Total |
|------|-------------|---------------|--------|-------|
| Analyze | ~5000 | ~4000 | ~1000 | ~10K |
| Vocab chunk 1 | ~5000 | ~2000 | ~500 | ~7.5K |
| Vocab chunk 2 | ~5000 | ~2000 | ~500 | ~7.5K |

At 12K TPM, each subsequent call triggers a ~60s rate-limit wait. Total: ~2-3 minutes for a long article. Acceptable — user already chose long-article mode.

## Files to modify

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `VocabTargetWords` field |
| `internal/llm/provider.go` | Add `ExtractVocab` to interface |
| `internal/llm/vocab.go` | New: types, prompt rendering, schema validation |
| `internal/llm/prompts/vocab_system.txt` | New: vocab-only system prompt |
| `internal/llm/prompts/vocab_user.tmpl` | New: vocab-only user template |
| `internal/llm/schema/vocab.json` | New: vocab response schema |
| `internal/llm/prompts/system.txt` | Add 500-word minimum for adapted |
| `internal/llm/groq/budget.go` | Add `VocabExtractOutputCap = 2000` |
| `internal/llm/groq/vocab.go` | New: Groq ExtractVocab implementation |
| `internal/llm/groq/analyze.go` | Refactor `chat()` to accept configurable maxTokens |
| `internal/llm/mock/mock.go` | Add ExtractVocab to mock |
| `internal/articles/chunk.go` | New: `chunkText` helper |
| `internal/articles/usecase.go` | Thread fullText, extractVocabChunks, mergeWords |
| `cmd/bot/main.go` | Wire VocabTargetWords from config |
| Telegram handler + i18n | New progress stage string |

## What does NOT change

- Short article flow (≤5000 tokens) — untouched
- Cache logic, safety flags, translation enrichment — untouched
- Main Analyze prompt and schema — untouched
- Database schema — no new tables/columns needed (words stored same way)

## Verification

1. Submit a short article (<5000 tokens) → behavior identical to current
2. Submit the test article (https://webdesignerhut.com/semantic-html-accessibility/, ~7694 words):
   - Choose "Truncate" → should see summary + adapted + 15-20 words (not 5)
   - Choose "Summarize" → same: enriched word list from full text
3. Check logs for `ExtractVocab` calls and word counts
4. Verify `VOCAB_TARGET_WORDS=10` env override reduces word count
5. Verify adapted_versions.current is ~500+ words for the long article
6. `make build` and `make test` pass
