package articles

import (
	"strings"
	"testing"
)

func TestChunkText_ShortText(t *testing.T) {
	text := "Hello world. This is short."
	chunks := chunkText(text, 5000)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != text {
		t.Errorf("expected full text, got %q", chunks[0])
	}
}

func TestChunkText_TwoChunks(t *testing.T) {
	// Build text that requires two chunks at 100-token budget.
	// Each paragraph ~30 tokens (120 chars).
	para := strings.Repeat("word ", 24) // 120 chars ~ 30 tokens
	text := para + "\n\n" + para + "\n\n" + para + "\n\n" + para

	chunks := chunkText(text, 100)
	if len(chunks) < 1 || len(chunks) > 2 {
		t.Fatalf("expected 1-2 chunks, got %d", len(chunks))
	}
	// Both chunks should be non-empty
	for i, c := range chunks {
		if strings.TrimSpace(c) == "" {
			t.Errorf("chunk %d is empty", i)
		}
	}
}

func TestChunkText_MaxTwoChunks(t *testing.T) {
	// Build very long text — should still get at most 2 chunks.
	para := strings.Repeat("word ", 24)
	var parts []string
	for i := 0; i < 20; i++ {
		parts = append(parts, para)
	}
	text := strings.Join(parts, "\n\n")

	chunks := chunkText(text, 100)
	if len(chunks) > 2 {
		t.Fatalf("expected at most 2 chunks, got %d", len(chunks))
	}
}

func TestChunkText_EmptyInput(t *testing.T) {
	chunks := chunkText("", 5000)
	if chunks != nil {
		t.Errorf("expected nil, got %v", chunks)
	}
}

func TestChunkText_ZeroBudget(t *testing.T) {
	chunks := chunkText("hello", 0)
	if chunks != nil {
		t.Errorf("expected nil, got %v", chunks)
	}
}
