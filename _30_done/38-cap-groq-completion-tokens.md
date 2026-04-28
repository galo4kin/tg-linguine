# Step 38 — cap-groq-completion-tokens

## Проблема

Шаги 36 и 37 не убрали 413: новый диагностический лог из шага 37 показал реальное сообщение Groq:

```
Request too large for model `llama-3.3-70b-versatile` ... on tokens per minute (TPM): Limit 12000, Requested 27455
```

Free-tier лимит — **12 000 токенов на запрос** (input + зарезервированный output). В `chatRequest` мы не задавали `max_tokens`, и Groq резервирует дефолтный output (~16K). При article=10K + system prompt + scaffolding output reserve ≈ 27K → 413.

## Что делаем

1. Добавить `MaxCompletionTokens int` в `chatRequest` (`internal/llm/groq/analyze.go`) и пробрасывать значение из вызывающих:
   - `Analyze` → 3000 (хватает на JSON со summary/words/adapted_versions).
   - `Adapt` → 1500.
   - `Summarize` → не больше `req.TargetTokens + 500`.
2. `MAX_TOKENS_PER_ARTICLE` envDefault: 10000 → **6000**. С 3K output + ~1K system prompt + 6K статья = ~10K, остаётся запас под scaffolding и known-words.
3. `summarizeInputBudget` const: 12000 → **8000**.
4. README синхронизировать.
5. Тесты — обновить груш-фикстуры/моки если читают тело запроса (groq/analyze_reference_test.go).

## DoD

- `make build` зелёный, `make test` зелёный.
- Один коммит `step 38: cap-groq-completion-tokens`.
- `pkill -f bin/tg-linguine` → watchdog поднимает свежий PID.
- Wikipedia AI: park → truncate → Analyze идёт без 413.
