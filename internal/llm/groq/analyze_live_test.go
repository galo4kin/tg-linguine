package groq

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nikita/tg-linguine/internal/llm"
)

// TestAnalyze_Live hits the real Groq API with a small reference article and
// asserts the response satisfies the schema and minimum content rules. It is
// gated behind GROQ_LIVE_TEST=1 so it never runs in CI; the point is to give
// a one-command sanity check that the model contract still holds.
//
// Run manually:
//
//	GROQ_LIVE_TEST=1 GROQ_API_KEY=gsk_xxx go test \
//	  ./internal/llm/groq/ -run Live -count=1 -v
//
// Costs a small number of Groq tokens per run.
func TestAnalyze_Live(t *testing.T) {
	if os.Getenv("GROQ_LIVE_TEST") != "1" {
		t.Skip("skipping live Groq test; set GROQ_LIVE_TEST=1 to enable")
	}
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		t.Fatal("GROQ_LIVE_TEST=1 but GROQ_API_KEY is empty")
	}

	body, err := os.ReadFile(filepath.Join("testdata", "reference_request_article.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	c := New(WithModel(DefaultModel))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := c.Analyze(ctx, apiKey, llm.AnalyzeRequest{
		TargetLanguage: "en",
		NativeLanguage: "ru",
		CEFR:           "B1",
		ArticleTitle:   "Why did the chicken cross the road?",
		ArticleText:    string(body),
	})
	if err != nil {
		t.Fatalf("live Analyze: %v", err)
	}

	// Contract sanity: the bug we just fixed was an empty `current` slipping
	// past validation. If the model regresses, this assertion catches it
	// before users hit "Что-то пошло не так".
	if resp.AdaptedVersions.Current == "" {
		t.Fatal("live response has empty adapted_versions.current — schema or model contract regressed")
	}
	if resp.SummaryTarget == "" || resp.SummaryNative == "" {
		t.Fatalf("live response missing summary: target=%q native=%q", resp.SummaryTarget, resp.SummaryNative)
	}
	if len(resp.Words) == 0 {
		t.Fatal("live response returned zero words — model is not pulling vocabulary from the article")
	}
	t.Logf("live ok: cefr=%s words=%d current_chars=%d",
		resp.CEFRDetected, len(resp.Words), len(resp.AdaptedVersions.Current))
}
