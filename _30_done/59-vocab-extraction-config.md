# Step 59: Add VOCAB_TARGET_WORDS config + LLM ExtractVocab types

## Goal
Add the configuration for target word count and create the LLM layer for vocabulary-only extraction.

## Context
Long articles lose vocabulary because the LLM only sees compressed text. We're adding a separate vocab-only LLM pass. This step sets up the foundation: config, types, prompts, schema, provider interface, Groq implementation, and mock.

Full spec: `docs/superpowers/specs/2026-04-29-vocab-extraction-improvement-design.md`

## What to do

### 1. Config
- `internal/config/config.go`: add `VocabTargetWords int` field, env `VOCAB_TARGET_WORDS`, default 20
- `cmd/bot/main.go`: wire `cfg.VocabTargetWords` → `ServiceDeps.VocabTargetWords`
- `internal/articles/usecase.go`: add `vocabTarget int` to `Service`, `VocabTargetWords int` to `ServiceDeps`, default to 20 when zero

### 2. LLM types + prompts + schema
- New `internal/llm/vocab.go`: `ExtractVocabRequest`, `ExtractVocabResponse`, embed prompts + schema, render/validate functions
- New `internal/llm/prompts/vocab_system.txt`: stripped-down system prompt (words only)
- New `internal/llm/prompts/vocab_user.tmpl`: template with `already_found_lemmas`, `vocab_target`
- New `internal/llm/schema/vocab.json`: schema with single `words` array

### 3. Provider interface + Groq + mock
- `internal/llm/provider.go`: add `ExtractVocab` to interface
- `internal/llm/groq/budget.go`: add `VocabExtractOutputCap = 2000`
- `internal/llm/groq/analyze.go`: refactor `chat()` to accept configurable `maxTokens`
- New `internal/llm/groq/vocab.go`: implement ExtractVocab
- `internal/llm/mock/mock.go`: add ExtractVocab to mock

## Definition of Done
- [ ] `VOCAB_TARGET_WORDS` env var parsed and wired through to articles.Service
- [ ] `ExtractVocab` method on Provider interface with Groq implementation
- [ ] Vocab-only prompts and JSON schema created
- [ ] Mock updated
- [ ] `make build` passes
