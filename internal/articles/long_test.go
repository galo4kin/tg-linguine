package articles_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/dictionary"
	"github.com/nikita/tg-linguine/internal/users"
)

func TestEstimateTokens_LenDiv4(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abcd", 1},        // 4 runes / 4 = 1
		{"abcde", 2},       // 5 runes / 4 = 2 (rounded up)
		{"привет", 2},      // 6 runes / 4 = 2
		{"привет!", 2},     // 7 runes / 4 = 2 (rounded up)
		{"a b c d e f g h", 4}, // 15 runes / 4 = 4
	}
	for _, c := range cases {
		if got := articles.EstimateTokens(c.in); got != c.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestApproxWordCount(t *testing.T) {
	cases := map[string]int{
		"":                       0,
		"   ":                    0,
		"hello":                  1,
		"hello world":            2,
		"hello   world\nfoo\tbar": 4,
		"один два три":           3,
	}
	for in, want := range cases {
		if got := articles.ApproxWordCount(in); got != want {
			t.Errorf("ApproxWordCount(%q) = %d, want %d", in, got, want)
		}
	}
}

// makeBody returns a string with exactly `runes` runes — enough to land on a
// specific EstimateTokens output. Words are space-separated so ApproxWordCount
// also resolves cleanly.
func makeBody(runes int) string {
	if runes <= 0 {
		return ""
	}
	// "word " is 5 runes. Build by repeating a short token then trimming.
	var sb strings.Builder
	sb.Grow(runes)
	for sb.Len() < runes {
		sb.WriteString("word ")
	}
	return sb.String()[:runes]
}

// TestAnalyzeArticle_RejectsLongArticleBeforeLLM covers the DoD:
//
//   - very long article → typed *TooLongError;
//   - the LLM is NOT called (no token spend);
//   - nothing is persisted to the articles table.
func TestAnalyzeArticle_RejectsLongArticleBeforeLLM(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)

	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)
	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	const limit = 1000
	// 4001 runes ≈ 1001 tokens — strictly above the limit.
	body := makeBody(4001)

	llmStub := &stubLLM{resp: sampleResponse()}
	svc := articles.NewService(articles.ServiceDeps{
		DB: db, Users: usersSvc, Languages: langs, Keys: keys,
		Extractor:    stubExtractor{out: articles.Extracted{URL: "https://l", URLHash: "hl", Title: "long", Content: body}},
		LLM:          llmStub,
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
		MaxTokens:    limit,
		Log:          slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})

	_, err := svc.AnalyzeArticle(context.Background(), userID, "https://l", nil)
	if err == nil {
		t.Fatalf("expected ErrTooLong, got nil")
	}
	if !errors.Is(err, articles.ErrTooLong) {
		t.Fatalf("expected errors.Is(err, ErrTooLong), got %v", err)
	}

	var tooLong *articles.TooLongError
	if !errors.As(err, &tooLong) {
		t.Fatalf("expected *TooLongError, got %T", err)
	}
	if tooLong.Limit != limit {
		t.Errorf("Limit = %d, want %d", tooLong.Limit, limit)
	}
	if tooLong.Tokens <= limit {
		t.Errorf("Tokens = %d, must be > %d to qualify as too-long", tooLong.Tokens, limit)
	}
	if tooLong.Words <= 0 {
		t.Errorf("Words = %d, expected > 0 for the user-facing message", tooLong.Words)
	}

	if llmStub.calls != 0 {
		t.Errorf("LLM was called %d times — must not be invoked when article is rejected", llmStub.calls)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("articles table got %d rows; rejected articles must not be persisted", n)
	}
}

// TestAnalyzeArticle_BoundaryAtLimitPasses covers the DoD's "edge case
// (exactly at limit) — passes". The token estimate must be <= limit, not <,
// so an article whose estimate lands precisely on the limit goes through.
func TestAnalyzeArticle_BoundaryAtLimitPasses(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)

	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)
	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	const limit = 1000
	// Body sized to land on exactly `limit` tokens via the rune/4 heuristic.
	body := makeBody(limit * 4)
	if got := articles.EstimateTokens(body); got != limit {
		t.Fatalf("test fixture: EstimateTokens(body) = %d, want %d", got, limit)
	}

	llmStub := &stubLLM{resp: sampleResponse()}
	svc := articles.NewService(articles.ServiceDeps{
		DB: db, Users: usersSvc, Languages: langs, Keys: keys,
		Extractor:    stubExtractor{out: articles.Extracted{URL: "https://b", URLHash: "hb", Title: "boundary", Content: body}},
		LLM:          llmStub,
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
		MaxTokens:    limit,
		Log:          slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})

	if _, err := svc.AnalyzeArticle(context.Background(), userID, "https://b", nil); err != nil {
		t.Fatalf("boundary article must pass, got err: %v", err)
	}
	if llmStub.calls != 1 {
		t.Errorf("expected LLM call=1 at boundary, got %d", llmStub.calls)
	}
}

// TestAnalyzeArticle_DefaultLimitActiveWhenZero proves the default kicks in
// when ServiceDeps leaves MaxTokens at zero — guards against accidentally
// shipping with the limit silently disabled.
func TestAnalyzeArticle_DefaultLimitActiveWhenZero(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)

	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)
	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	// Build an article that exceeds the default 7000-token budget.
	body := makeBody((articles.DefaultMaxTokensPerArticle + 100) * 4)

	llmStub := &stubLLM{resp: sampleResponse()}
	svc := articles.NewService(articles.ServiceDeps{
		DB: db, Users: usersSvc, Languages: langs, Keys: keys,
		Extractor:    stubExtractor{out: articles.Extracted{URL: "https://huge", URLHash: "hh", Title: "huge", Content: body}},
		LLM:          llmStub,
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
		// MaxTokens left at zero on purpose — service must fall back to the
		// DefaultMaxTokensPerArticle constant rather than disabling the gate.
		Log: slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})

	_, err := svc.AnalyzeArticle(context.Background(), userID, "https://huge", nil)
	if !errors.Is(err, articles.ErrTooLong) {
		t.Fatalf("expected ErrTooLong with default limit, got %v", err)
	}
	if llmStub.calls != 0 {
		t.Errorf("LLM must not be invoked on rejected article, calls=%d", llmStub.calls)
	}
}
