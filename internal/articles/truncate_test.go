package articles_test

import (
	"strings"
	"testing"

	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/llm"
)

func TestTruncateAtParagraph_BelowBudgetReturnsInputUntouched(t *testing.T) {
	in := "alpha beta\n\ngamma delta"
	out, pct := articles.TruncateAtParagraph(in, 1000)
	if out != in {
		t.Errorf("body changed: got %q want %q", out, in)
	}
	if pct != 100 {
		t.Errorf("percent = %d, want 100", pct)
	}
}

func TestTruncateAtParagraph_CutsOnParagraphBoundary(t *testing.T) {
	// Each paragraph ≈ 24 runes ≈ 6 tokens. Budget of 12 tokens fits 2.
	body := "alpha beta gamma delta\n\nepsilon zeta eta theta\n\niota kappa lambda mu\n\nnu xi omicron pi"
	out, pct := articles.TruncateAtParagraph(body, 12)

	if !strings.HasPrefix(body, out) {
		t.Errorf("truncated text must be a prefix of input; got %q", out)
	}
	if strings.Contains(out, "iota") {
		t.Errorf("truncate took too much: %q", out)
	}
	if llm.EstimateTokens(out) > 12 {
		t.Errorf("estimated tokens %d exceeds budget 12 for %q", llm.EstimateTokens(out), out)
	}
	if pct >= 100 || pct <= 0 {
		t.Errorf("percent should be in (0,100), got %d", pct)
	}
}

func TestTruncateAtParagraph_FirstParagraphOversizedFallsBackToRunePrefix(t *testing.T) {
	// Single huge paragraph above the budget — no \n\n to split on.
	body := strings.Repeat("word ", 200) // 1000 runes ≈ 250 tokens
	out, pct := articles.TruncateAtParagraph(body, 10)

	if out == "" {
		t.Fatalf("truncate returned empty for oversized first paragraph")
	}
	if llm.EstimateTokens(out) > 10 {
		t.Errorf("rune-prefix overshot budget: tokens=%d", llm.EstimateTokens(out))
	}
	if pct >= 100 {
		t.Errorf("percent should be < 100 after truncation, got %d", pct)
	}
}

func TestTruncateAtParagraph_EmptyInputIsPassThrough(t *testing.T) {
	out, pct := articles.TruncateAtParagraph("", 100)
	if out != "" || pct != 100 {
		t.Errorf("empty input → got %q %d", out, pct)
	}
}

func TestTruncateAtParagraph_ZeroOrNegativeBudgetIsPassThrough(t *testing.T) {
	const body = "some text"
	out, pct := articles.TruncateAtParagraph(body, 0)
	if out != body || pct != 100 {
		t.Errorf("zero budget should pass through; got %q %d", out, pct)
	}
}
