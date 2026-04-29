# Step 60: Vocab chunking, merging, and integration in runAnalysis

## Goal
Wire the vocab extraction into the article processing pipeline: chunk full text, call ExtractVocab, merge words, update progress reporting.

## Context
Step 59 added the config and LLM layer. This step integrates it into articles.Service so long articles actually get more vocabulary extracted.

Full spec: `docs/superpowers/specs/2026-04-29-vocab-extraction-improvement-design.md`

## What to do

### 1. Chunking helper
- New `internal/articles/chunk.go`: `chunkText(text string, maxTokens int) []string`
  - Split into up to 2 paragraph-bounded chunks using TruncateAtParagraph
  - Chunk 1: first maxTokens tokens; Chunk 2: next maxTokens tokens of remainder

### 2. Integration in runAnalysis
- `internal/articles/usecase.go`:
  - Add `fullText string` parameter to `runAnalysis`
  - Add private `extractVocabChunks` method on Service
  - Add private `mergeWords` helper (dedup by lowercased lemma, cap at vocabTarget)
  - Update `AnalyzeExtracted` to pass `extracted.Content` (original from pending store) as fullText
  - Update `AnalyzeArticle` to pass `""` as fullText (short articles, no extra pass)
  - Add `StageExtractingVocab` progress stage

### 3. Telegram handler + i18n
- Add i18n key for StageExtractingVocab
- Update handler to display new progress stage

### 4. System prompt
- `internal/llm/prompts/system.txt`: add 500-word minimum for adapted_versions.current

## Definition of Done
- [ ] Long articles get vocab extracted from full text (up to 2 chunks)
- [ ] Words merged and deduplicated by lemma
- [ ] Total word count respects VOCAB_TARGET_WORDS cap
- [ ] Progress message shows "Extracting vocab" stage
- [ ] Adapted text instruction updated
- [ ] `make build` passes
- [ ] `make test` passes
