# 35 — code review

Plain refactor pass over the diff for steps 34–35. Goal: spot dead code,
duplication, leaky abstractions, naming smells. Bug-fix changes are out of
scope for the review.

## Findings

### 1. Test fixtures live in two places without a shared helper

- `internal/llm/groq/testdata/reference_*.json` (new in step 35)
- `internal/llm/mock/fixtures/analyze_*.json` (existing)
- `internal/llm/testdata/{valid,missing_field,wrong_type,bad_enum}.json`
  (existing)

All three fixture trees describe the same `analysis.json` schema. Today they
diverge: `mock/fixtures/analyze_flagged.json` had to grow a `[redacted]`
placeholder in step 34 to satisfy the new `current` minLength constraint,
purely because there is no shared "build me a valid AnalyzeResponse, vary
field X" helper. Realistic risk: next schema tightening will silently miss
one of the trees.

Not a refactor I'd do today — three trees is small enough to keep grep-ping.
Worth revisiting when adding a fourth fixture set or when the schema gets
another required field.

### 2. `chatBody` helper duplicated across groq tests

`internal/llm/groq/analyze_test.go` defines `chatBody(content string) string`,
and `analyze_reference_test.go` defines a near-identical `okBody(inner string) serverResponse`.
They serve slightly different signatures (string vs serverResponse) so a
straight unify is awkward. Acceptable duplication.

### 3. `urlHandlerEnv` test scaffolding is a candidate for extraction

`internal/telegram/handlers/url_integration_test.go` builds a complete
in-memory wiring stack (SQLite + cipher + repos + bundle + httptest Telegram).
Lines ~190-280 are the bulk of it. Future handler tests (settings,
onboarding, study) will likely want the same stack. Worth promoting to
`internal/telegram/handlers/test_helpers_test.go` when the next handler test
ships — premature today (one consumer).

### 4. `articleErrorMessageID` switch is starting to look like a registry

[`internal/telegram/handlers/url.go::articleErrorMessageID`](internal/telegram/handlers/url.go#L199)
has 13 explicit branches. Each maps a typed error to an i18n key. A `map[error]string`
plus `errors.Is` loop would be slightly more compact but loses compile-time
exhaustiveness (which is the whole point — see step 34 root cause). Switch
form is correct for this. No change.

### 5. `bot.log` accumulating unrelated regression noise

Outside the diff, but worth noting: `bot.log` shows three weeks of mixed
WARNs (migration dirty state, schema validation failures, "url: regen") which
made root-causing step 34 slower. There is no operational dashboard yet —
just `tail bot.log | grep`. Not actionable until someone is willing to write
a small log-filtering tool.

## No refactor tasks queued

None of the above warrant a numbered refactor task right now. Re-evaluate at
step 40.
