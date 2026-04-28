package articles_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/dictionary"
	"github.com/nikita/tg-linguine/internal/llm"
	"github.com/nikita/tg-linguine/internal/users"
)

func TestBlocklist_ParsesAndMatchesSubdomains(t *testing.T) {
	bl := articles.NewBlocklistFromText(`# header
example.com
*.tracker.invalid
shady.test  # inline comment

# blank line above; a leading dot should also be tolerated
.dot-prefix.test
EXAMPLE.NET
`)
	if got := bl.Size(); got != 5 {
		t.Fatalf("Size() = %d, want 5 (example.com, tracker.invalid, shady.test, dot-prefix.test, example.net)", got)
	}

	cases := []struct {
		host string
		want bool
	}{
		{"example.com", true},
		{"www.example.com", true},
		{"deep.www.example.com", true},
		{"badexample.com", false}, // suffix without preceding dot must not match
		{"tracker.invalid", true},
		{"a.tracker.invalid", true},
		{"shady.test", true},
		{"foo.shady.test", true},
		{"dot-prefix.test", true},
		{"example.net", true},   // case-insensitive entry
		{"EXAMPLE.NET", true},
		{"unknown.example", false},
		{"", false},
	}
	for _, c := range cases {
		if got := bl.Contains(c.host); got != c.want {
			t.Errorf("Contains(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestBlocklist_MatchURL(t *testing.T) {
	bl := articles.NewBlocklistFromText("example.com\n")
	cases := map[string]bool{
		"https://example.com/path":          true,
		"https://www.example.com/article":   true,
		"http://EXAMPLE.com":                true,
		"https://example.com:8080/x":        true,
		"https://example.org":               false,
		"":                                  false,
		"not a url":                         false,
	}
	for in, want := range cases {
		if got := bl.MatchURL(in); got != want {
			t.Errorf("MatchURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBlocklist_NilSafe(t *testing.T) {
	var bl *articles.Blocklist
	if bl.Contains("anything.com") {
		t.Fatalf("nil blocklist must reject nothing")
	}
	if bl.MatchURL("https://anything.com") {
		t.Fatalf("nil MatchURL must return false")
	}
	if bl.Size() != 0 {
		t.Fatalf("nil Size() must be 0")
	}
}

// TestAnalyzeArticle_BlockedSource_NoNetworkCall covers the DoD: a URL
// matching the blocklist short-circuits the pipeline before the extractor
// runs (and obviously before the LLM does).
func TestAnalyzeArticle_BlockedSource_NoNetworkCall(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)

	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)
	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	llmStub := &stubLLM{resp: sampleResponse()}
	extractorCalls := 0
	extractor := stubExtractorFn(func(ctx context.Context, url string) (articles.Extracted, error) {
		extractorCalls++
		return articles.Extracted{}, nil
	})

	bl := articles.NewBlocklistFromText("blocked.test\n")
	svc := articles.NewService(articles.ServiceDeps{
		DB: db, Users: usersSvc, Languages: langs, Keys: keys,
		Extractor:    extractor,
		LLM:          llmStub,
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
		Blocklist:    bl,
		Log:          slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})

	_, err := svc.AnalyzeArticle(context.Background(), userID, "https://sub.blocked.test/article", nil)
	if !errors.Is(err, articles.ErrBlockedSource) {
		t.Fatalf("expected ErrBlockedSource, got %v", err)
	}
	if extractorCalls != 0 {
		t.Errorf("extractor must not be invoked when source is blocked, calls=%d", extractorCalls)
	}
	if llmStub.calls != 0 {
		t.Errorf("LLM must not be invoked when source is blocked, calls=%d", llmStub.calls)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("blocked source must not persist any article, got %d rows", n)
	}
}

// TestAnalyzeArticle_LLMSafetyFlags_DropsArticle pins the second half of
// step 27: a non-empty safety_flags array means "do not store" — the
// articles table stays empty.
func TestAnalyzeArticle_LLMSafetyFlags_DropsArticle(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)

	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)
	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	flagged := sampleResponse()
	flagged.SafetyFlags = []string{"adult"}
	llmStub := &stubLLM{resp: flagged}

	svc := articles.NewService(articles.ServiceDeps{
		DB: db, Users: usersSvc, Languages: langs, Keys: keys,
		Extractor:    stubExtractor{out: articles.Extracted{URL: "https://x", URLHash: "h", Title: "t", Content: "body"}},
		LLM:          llmStub,
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
		Log:          slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})

	_, err := svc.AnalyzeArticle(context.Background(), userID, "https://x", nil)
	if !errors.Is(err, articles.ErrBlockedContent) {
		t.Fatalf("expected ErrBlockedContent, got %v", err)
	}
	// The LLM was invoked exactly once — we cannot avoid that, but we still
	// must not persist anything once it flags the article.
	if llmStub.calls != 1 {
		t.Errorf("LLM should be called once before the safety check, got %d", llmStub.calls)
	}

	for _, q := range []string{
		`SELECT COUNT(*) FROM articles`,
		`SELECT COUNT(*) FROM article_words`,
		`SELECT COUNT(*) FROM user_word_status`,
		`SELECT COUNT(*) FROM dictionary_words`,
	} {
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", q, err)
		}
		if n != 0 {
			t.Errorf("flagged article must persist nothing, got %d for %q", n, q)
		}
	}
}

// Asserts the source-block does not break the LLM error path: a non-blocked
// URL with a working LLM still goes through cleanly, even when a blocklist
// is configured. (Regression guard against accidentally short-circuiting on
// every URL.)
func TestAnalyzeArticle_BlocklistDoesNotInterfereWithCleanURLs(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)

	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)
	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	llmStub := &stubLLM{resp: sampleResponse()}
	bl := articles.NewBlocklistFromText("only-this-blocked.test\n")
	svc := articles.NewService(articles.ServiceDeps{
		DB: db, Users: usersSvc, Languages: langs, Keys: keys,
		Extractor:    stubExtractor{out: articles.Extracted{URL: "https://allowed.test/post", NormalizedURL: "https://allowed.test/post", URLHash: "h", Title: "ok", Content: "body"}},
		LLM:          llmStub,
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
		Blocklist:    bl,
		Log:          slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})

	if _, err := svc.AnalyzeArticle(context.Background(), userID, "https://allowed.test/post", nil); err != nil {
		t.Fatalf("clean URL must succeed: %v", err)
	}
	if llmStub.calls != 1 {
		t.Errorf("clean URL should reach LLM, got calls=%d", llmStub.calls)
	}
}

// guards against a regression where an LLM error would be confused with a
// safety flag — an LLM rate-limit must surface as ErrRateLimited, not
// ErrBlockedContent.
func TestAnalyzeArticle_LLMErrorNotConfusedWithSafetyFlag(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)
	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)
	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	svc := articles.NewService(articles.ServiceDeps{
		DB: db, Users: usersSvc, Languages: langs, Keys: keys,
		Extractor:    stubExtractor{out: articles.Extracted{URL: "https://ok.test", URLHash: "h", Title: "t", Content: "body"}},
		LLM:          &stubLLM{err: llm.ErrRateLimited},
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
	})
	_, err := svc.AnalyzeArticle(context.Background(), userID, "https://ok.test", nil)
	if !errors.Is(err, llm.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	if errors.Is(err, articles.ErrBlockedContent) {
		t.Fatalf("rate-limit must not surface as blocked content")
	}
}
