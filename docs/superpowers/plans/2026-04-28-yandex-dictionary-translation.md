# Yandex Dictionary Translation Integration

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace LLM-generated `translation_native` with Yandex Dictionary API translations, keeping LLM as fallback.

**Architecture:** New `internal/translation/` package exposes a `Translator` interface with a Yandex Dictionary HTTP client. `articles.Service` gains an optional `translator` field; after `llm.Analyze()` returns words, each lemma is enriched in-place before DB insert. If the API key is unset or a lookup fails, the LLM translation is kept.

**Tech Stack:** Go stdlib `net/http`, Yandex Dictionary REST API v1, `httptest` for unit tests.

---

## File map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `internal/translation/translator.go` | `Translator` interface |
| Create | `internal/translation/yandex.go` | Yandex Dictionary HTTP client |
| Create | `internal/translation/yandex_test.go` | Unit tests (httptest mock server) |
| Modify | `internal/config/config.go` | Add `YandexDictAPIKey` env field |
| Modify | `internal/articles/usecase.go` | Add `translator` field + enrichment loop |
| Modify | `cmd/bot/main.go` | Construct client and inject into ServiceDeps |

---

### Task 1: Translator interface + Yandex client

**Files:**
- Create: `internal/translation/translator.go`
- Create: `internal/translation/yandex.go`
- Create: `internal/translation/yandex_test.go`

- [ ] **Step 1.1: Write failing tests**

Create `internal/translation/yandex_test.go`:

```go
package translation_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nikita/tg-linguine/internal/translation"
)

func yandexResponse(translations []string) translation.YandexResponse {
	trs := make([]translation.YandexTr, len(translations))
	for i, t := range translations {
		trs[i] = translation.YandexTr{Text: t}
	}
	return translation.YandexResponse{
		Def: []translation.YandexDef{{
			Tr: trs,
		}},
	}
}

func TestYandexClient_Translate_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("text") != "compelling" {
			t.Errorf("unexpected text param: %s", r.URL.Query().Get("text"))
		}
		if r.URL.Query().Get("lang") != "en-ru" {
			t.Errorf("unexpected lang param: %s", r.URL.Query().Get("lang"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(yandexResponse([]string{"убедительный", "неотразимый", "принудительный"}))
	}))
	defer srv.Close()

	client := translation.NewYandex("test-key", translation.WithBaseURL(srv.URL))
	got, err := client.Translate(context.Background(), "compelling", "en", "ru")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "убедительный, неотразимый, принудительный"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestYandexClient_Translate_EmptyDef(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(translation.YandexResponse{})
	}))
	defer srv.Close()

	client := translation.NewYandex("test-key", translation.WithBaseURL(srv.URL))
	got, err := client.Translate(context.Background(), "xyzzy", "en", "ru")
	if err != nil {
		t.Fatalf("unexpected error on empty result: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestYandexClient_Translate_TrimsToThree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(yandexResponse([]string{"a", "b", "c", "d", "e"}))
	}))
	defer srv.Close()

	client := translation.NewYandex("test-key", translation.WithBaseURL(srv.URL))
	got, err := client.Translate(context.Background(), "word", "en", "ru")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "a, b, c"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
```

- [ ] **Step 1.2: Run tests — expect compile failure**

```bash
cd /Users/thebrain/Projects/tg-linguine && go test ./internal/translation/... 2>&1 | head -20
```

Expected: `cannot find package` or similar compile error.

- [ ] **Step 1.3: Create interface file**

Create `internal/translation/translator.go`:

```go
package translation

import "context"

// Translator looks up a single word and returns its translation in the target
// native language. Returns an empty string (no error) when the word is not
// found. Implementations must be safe for concurrent use.
type Translator interface {
	Translate(ctx context.Context, word, fromLang, toLang string) (string, error)
}
```

- [ ] **Step 1.4: Create Yandex client**

Create `internal/translation/yandex.go`:

```go
package translation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const yandexDictBaseURL = "https://dictionary.yandex.net/api/v1/dicservice.json/lookup"

// YandexResponse is the top-level JSON structure returned by the Yandex
// Dictionary API. Fields are exported so tests can construct mock responses.
type YandexResponse struct {
	Def []YandexDef `json:"def"`
}

// YandexDef is one dictionary entry (one part-of-speech group).
type YandexDef struct {
	Tr []YandexTr `json:"tr"`
}

// YandexTr is one translation variant inside a definition.
type YandexTr struct {
	Text string `json:"text"`
}

// YandexClient calls the Yandex Dictionary REST API.
type YandexClient struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

type yandexOption func(*YandexClient)

// WithBaseURL overrides the API endpoint — used in tests to point at a local
// httptest server.
func WithBaseURL(u string) yandexOption {
	return func(c *YandexClient) { c.baseURL = u }
}

// NewYandex creates a Yandex Dictionary client. apiKey must be non-empty.
func NewYandex(apiKey string, opts ...yandexOption) *YandexClient {
	c := &YandexClient{
		apiKey:  apiKey,
		baseURL: yandexDictBaseURL,
		http:    &http.Client{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Translate returns the top (up to 3) translations of word from fromLang to
// toLang joined by ", ". Returns "" when the word is not in the dictionary.
func (c *YandexClient) Translate(ctx context.Context, word, fromLang, toLang string) (string, error) {
	params := url.Values{
		"key":  {c.apiKey},
		"lang": {fromLang + "-" + toLang},
		"text": {word},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"?"+params.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("yandex dict: build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("yandex dict: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("yandex dict: status %d", resp.StatusCode)
	}

	var result YandexResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("yandex dict: decode: %w", err)
	}

	if len(result.Def) == 0 || len(result.Def[0].Tr) == 0 {
		return "", nil
	}

	trs := result.Def[0].Tr
	if len(trs) > 3 {
		trs = trs[:3]
	}
	parts := make([]string, len(trs))
	for i, t := range trs {
		parts[i] = t.Text
	}
	return strings.Join(parts, ", "), nil
}
```

- [ ] **Step 1.5: Run tests — expect pass**

```bash
cd /Users/thebrain/Projects/tg-linguine && go test ./internal/translation/... -v
```

Expected: all 3 tests PASS.

- [ ] **Step 1.6: Build check**

```bash
cd /Users/thebrain/Projects/tg-linguine && make build 2>&1
```

Expected: clean build.

---

### Task 2: Config field

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 2.1: Add field to Config struct**

In `internal/config/config.go`, add after the `AdminUserID` field:

```go
// YandexDictAPIKey enables Yandex Dictionary lookups for word translations.
// When empty the LLM-generated translation is used as-is.
YandexDictAPIKey string `env:"YANDEX_DICT_API_KEY"`
```

- [ ] **Step 2.2: Build check**

```bash
cd /Users/thebrain/Projects/tg-linguine && make build 2>&1
```

Expected: clean build.

---

### Task 3: Enrich words in articles service

**Files:**
- Modify: `internal/articles/usecase.go`

- [ ] **Step 3.1: Add Translator to Service struct and ServiceDeps**

In `internal/articles/usecase.go`, add to the `Service` struct (after `llm` field):

```go
translator translation.Translator
```

Add to `ServiceDeps` struct (after `LLM` field):

```go
// Translator enriches LLM-generated word translations via an external
// dictionary API. Nil disables enrichment — LLM translations are used as-is.
Translator translation.Translator
```

Add import `"github.com/nikita/tg-linguine/internal/translation"` to the import block.

Update `NewService` to wire the field (inside the `return &Service{...}` literal):

```go
translator: d.Translator,
```

- [ ] **Step 3.2: Add enrichment loop after LLM call**

In `runAnalysis`, after the safety-flags check (after `return nil, ErrBlockedContent` block, before `progress(onProgress, StagePersisting)`), add:

```go
if s.translator != nil {
    for i := range resp.Words {
        t, err := s.translator.Translate(ctx, resp.Words[i].Lemma, languageCode, user.InterfaceLanguage)
        if err != nil {
            if s.log != nil {
                s.log.Warn("yandex dict: translation failed, using LLM fallback",
                    "lemma", resp.Words[i].Lemma, "err", err)
            }
            continue
        }
        if t != "" {
            resp.Words[i].TranslationNative = t
        }
    }
}
```

- [ ] **Step 3.3: Build check**

```bash
cd /Users/thebrain/Projects/tg-linguine && make build 2>&1
```

Expected: clean build.

---

### Task 4: Wire client in main.go

**Files:**
- Modify: `cmd/bot/main.go`

- [ ] **Step 4.1: Construct Yandex client and inject**

In `cmd/bot/main.go`, after the `groqClient` block and before `extractor`, add:

```go
var yandexTranslator translation.Translator
if cfg.YandexDictAPIKey != "" {
    yandexTranslator = translation.NewYandex(cfg.YandexDictAPIKey)
    log.Info("yandex dictionary: enabled")
} else {
    log.Info("yandex dictionary: disabled (YANDEX_DICT_API_KEY not set)")
}
```

Add `"github.com/nikita/tg-linguine/internal/translation"` to the import block.

In `articles.NewService(articles.ServiceDeps{...})`, add:

```go
Translator: yandexTranslator,
```

- [ ] **Step 4.2: Build check**

```bash
cd /Users/thebrain/Projects/tg-linguine && make build 2>&1
```

Expected: clean build.

- [ ] **Step 4.3: Run full test suite**

```bash
cd /Users/thebrain/Projects/tg-linguine && make test 2>&1
```

Expected: all tests pass.

---

### Task 5: Create CLAUDE.md task file and commit

- [ ] **Step 5.1: Create task file**

Create `_10_todo/44-yandex-dict-translation.md` with this content, then immediately move it to `_20_in_progress/`:

```bash
mv _10_todo/44-yandex-dict-translation.md _20_in_progress/44-yandex-dict-translation.md
```

- [ ] **Step 5.2: Move to done and commit**

```bash
mv _20_in_progress/44-yandex-dict-translation.md _30_done/44-yandex-dict-translation.md
git add internal/translation/ internal/config/config.go internal/articles/usecase.go cmd/bot/main.go _30_done/44-yandex-dict-translation.md docs/superpowers/
git commit -m "step 44: yandex-dict-translation"
```

- [ ] **Step 5.3: Kill bot so watchdog restarts with new binary**

```bash
pkill -f bin/tg-linguine
```

Then verify new PID after ~70s:

```bash
ps aux | grep tg-linguine | grep -v grep
```

---

## Verification

1. Add `YANDEX_DICT_API_KEY=<your-key>` to `.env`
2. Restart bot (watchdog picks it up)
3. Send an English article to the bot
4. Check word cards — translations should be correct Russian words (no hallucinations)
5. Remove `YANDEX_DICT_API_KEY` from `.env`, restart — bot should still work using LLM translations
