package articles

import (
	"strings"

	"github.com/nikita/tg-linguine/internal/llm"
)

// TruncateAtParagraph cuts text at the latest paragraph boundary (\n\n) such
// that the cumulative estimated token count stays at or below maxTokens.
//
// It returns the truncated text and `percentKept`, the share of original
// runes preserved (0–100, rounded down). When the very first paragraph
// already overshoots maxTokens, it falls back to a rune-prefix cut sized to
// the budget so the user still gets *something* analyzable. An empty input
// or non-positive budget yields the input verbatim and percentKept=100.
func TruncateAtParagraph(text string, maxTokens int) (string, int) {
	if text == "" || maxTokens <= 0 {
		return text, 100
	}
	if llm.EstimateTokens(text) <= maxTokens {
		return text, 100
	}

	paragraphs := strings.Split(text, "\n\n")
	var kept strings.Builder
	for i, p := range paragraphs {
		candidate := p
		if i > 0 {
			candidate = "\n\n" + p
		}
		if llm.EstimateTokens(kept.String()+candidate) > maxTokens {
			break
		}
		kept.WriteString(candidate)
	}

	out := strings.TrimSpace(kept.String())
	if out == "" {
		// First paragraph alone overshoots: rune-prefix cut sized to the
		// budget. EstimateTokens is (runes+3)/4, so ~4 runes per token.
		runeBudget := maxTokens * 4
		runes := []rune(text)
		if runeBudget > len(runes) {
			runeBudget = len(runes)
		}
		out = strings.TrimSpace(string(runes[:runeBudget]))
	}

	totalRunes := len([]rune(text))
	keptRunes := len([]rune(out))
	percent := 100
	if totalRunes > 0 {
		percent = keptRunes * 100 / totalRunes
	}
	return out, percent
}
