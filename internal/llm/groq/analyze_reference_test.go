package groq

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nikita/tg-linguine/internal/llm"
)

// TestAnalyze_ReferenceArticle drives groq.Client end-to-end against a
// realistic article fixture and the kind of canonical Groq response that the
// service receives in production. The fixtures under testdata/reference_*
// double as documentation of the contract: if the schema or response shape
// changes, this test catches it before users do.
//
// Cases include the two regression-prone branches we just fixed:
//   - empty `choices` array → must surface as `llm.ErrUnavailable`
//     (previously bubbled up as a bare error and showed "что-то пошло не так")
//   - empty `adapted_versions.current` → must be rejected by the schema and
//     after one retry surface as `llm.ErrSchemaInvalid`
//     (previously slipped through and produced an unrenderable card)
func TestAnalyze_ReferenceArticle(t *testing.T) {
	article := mustReferenceFile(t, "reference_request_article.txt")
	okInner := mustReferenceFile(t, "reference_response_ok.json")
	emptyCurrentInner := mustReferenceFile(t, "reference_response_empty_current.json")

	type expect struct {
		err     error // errors.Is target; nil = success
		minWords int  // min len(Words) on success
	}

	cases := []struct {
		name      string
		responses []serverResponse // sequenced replies for sequential POSTs
		expect    expect
	}{
		{
			name:      "happy path with reference article",
			responses: []serverResponse{okBody(okInner)},
			expect:    expect{minWords: 1},
		},
		{
			name: "retry then ok",
			responses: []serverResponse{
				okBody("not even close to JSON"),
				okBody(okInner),
			},
			expect: expect{minWords: 1},
		},
		{
			name: "schema invalid both times",
			responses: []serverResponse{
				okBody("not even close to JSON"),
				okBody("still nope"),
			},
			expect: expect{err: llm.ErrSchemaInvalid},
		},
		{
			name: "empty current after retry",
			responses: []serverResponse{
				okBody(emptyCurrentInner),
				okBody(emptyCurrentInner),
			},
			expect: expect{err: llm.ErrSchemaInvalid},
		},
		{
			name: "empty choices",
			responses: []serverResponse{
				{status: 200, body: `{"choices":[]}`},
			},
			expect: expect{err: llm.ErrUnavailable},
		},
		{
			name: "401 unauthorized",
			responses: []serverResponse{
				{status: 401, body: `{}`},
			},
			expect: expect{err: llm.ErrInvalidAPIKey},
		},
		{
			name: "429 too many",
			responses: []serverResponse{
				{status: 429, body: `{}`},
			},
			expect: expect{err: llm.ErrRateLimited},
		},
		{
			name: "503 unavailable (after retries)",
			responses: []serverResponse{
				{status: 503, body: `{}`},
				{status: 503, body: `{}`},
				{status: 503, body: `{}`},
			},
			expect: expect{err: llm.ErrUnavailable},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seq := tc.responses
			callIdx := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if callIdx >= len(seq) {
					t.Fatalf("server got call %d but only %d responses prepared", callIdx+1, len(seq))
				}
				resp := seq[callIdx]
				callIdx++
				if resp.status != 0 && resp.status != 200 {
					w.WriteHeader(resp.status)
				}
				_, _ = io.WriteString(w, resp.body)
			}))
			defer srv.Close()

			// Zero backoff so the 5xx-retry case finishes in milliseconds.
			// Two retries (= three attempts) matches defaultBackoff's length;
			// the 503 case below relies on this to exhaust retries.
			c := New(WithBaseURL(srv.URL), WithBackoff([]time.Duration{0, 0}))
			got, err := c.Analyze(context.Background(), "k", llm.AnalyzeRequest{
				TargetLanguage: "en",
				NativeLanguage: "ru",
				CEFR:           "B1",
				ArticleTitle:   "Why did the chicken cross the road?",
				ArticleText:    article,
			})

			if tc.expect.err != nil {
				if !errors.Is(err, tc.expect.err) {
					t.Fatalf("expected %v, got %v", tc.expect.err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got.Words) < tc.expect.minWords {
				t.Fatalf("expected ≥%d words, got %d", tc.expect.minWords, len(got.Words))
			}
			if got.AdaptedVersions.Current == "" {
				t.Fatal("adapted_versions.current is empty on success path — schema must have failed to enforce minLength")
			}
		})
	}
}

type serverResponse struct {
	status int    // 0 means default 200
	body   string // raw response body
}

// okBody wraps an inner JSON string in the canonical Groq chat-completions
// envelope. Sharing this with analyze_test.go's chatBody helper would couple
// these tests; keeping a small dedicated helper keeps the reference test
// independent.
func okBody(inner string) serverResponse {
	body, _ := json.Marshal(chatResponse{
		Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: inner}}},
	})
	return serverResponse{status: 200, body: string(body)}
}

func mustReferenceFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
