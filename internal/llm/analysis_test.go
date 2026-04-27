package llm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestValidateAnalysisJSON_Valid(t *testing.T) {
	if err := ValidateAnalysisJSON(loadFixture(t, "valid.json")); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateAnalysisJSON_MissingField(t *testing.T) {
	err := ValidateAnalysisJSON(loadFixture(t, "missing_field.json"))
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("expected ErrSchemaInvalid, got %v", err)
	}
}

func TestValidateAnalysisJSON_WrongType(t *testing.T) {
	err := ValidateAnalysisJSON(loadFixture(t, "wrong_type.json"))
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("expected ErrSchemaInvalid, got %v", err)
	}
}

func TestValidateAnalysisJSON_BadEnum(t *testing.T) {
	err := ValidateAnalysisJSON(loadFixture(t, "bad_enum.json"))
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("expected ErrSchemaInvalid, got %v", err)
	}
}

func TestValidateAnalysisJSON_Garbage(t *testing.T) {
	err := ValidateAnalysisJSON([]byte("not json"))
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("expected ErrSchemaInvalid, got %v", err)
	}
}

func TestRenderUserPrompt_KnownWords(t *testing.T) {
	out, err := RenderUserPrompt(AnalyzeRequest{
		TargetLanguage: "en",
		NativeLanguage: "ru",
		CEFR:           "B1",
		KnownWords:     []string{"the", "a", "is"},
		ArticleTitle:   "Title",
		ArticleText:    "Body",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "the, a, is") {
		t.Fatalf("expected known words list in prompt, got: %s", out)
	}
	if !strings.Contains(out, "Title") || !strings.Contains(out, "Body") {
		t.Fatalf("expected title/body in prompt, got: %s", out)
	}
}

func TestRenderUserPrompt_NoKnownWords(t *testing.T) {
	out, err := RenderUserPrompt(AnalyzeRequest{
		TargetLanguage: "en",
		NativeLanguage: "ru",
		CEFR:           "A1",
		ArticleTitle:   "T",
		ArticleText:    "B",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "<none>") {
		t.Fatalf("expected <none> placeholder, got: %s", out)
	}
}

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abcd", 1},
		{"abcde", 2},
		{strings.Repeat("a", 400), 100},
	}
	for _, tc := range cases {
		if got := EstimateTokens(tc.in); got != tc.want {
			t.Errorf("EstimateTokens(%q)=%d want %d", tc.in, got, tc.want)
		}
	}
}
