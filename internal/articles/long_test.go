package articles_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/dictionary"
	"github.com/nikita/tg-linguine/internal/llm"
	"github.com/nikita/tg-linguine/internal/llm/mock"
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
		if got := llm.EstimateTokens(c.in); got != c.want {
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
	var sb strings.Builder
	sb.Grow(runes)
	for sb.Len() < runes {
		sb.WriteString("word ")
	}
	return sb.String()[:runes]
}

// TestAnalyzeArticle_LongArticleParksInsteadOfErroring covers the new
// contract introduced in step 36: an article that exceeds the per-request
// token budget is parked in the in-memory pending store and returned as a
// non-nil LongPending — not as an error. The LLM is NOT called and nothing
// is persisted to the articles table.
func TestAnalyzeArticle_LongArticleParksInsteadOfErroring(t *testing.T) {
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
	body := makeBody(4001) // ~1001 tokens via len/4 heuristic — strictly above limit.

	llmStub := &mock.Provider{AnalyzeResp: sampleResponse()}
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

	result, err := svc.AnalyzeArticle(context.Background(), userID, "https://l", nil)
	if err != nil {
		t.Fatalf("AnalyzeArticle returned error %v; long articles must succeed and park", err)
	}
	if result.Article != nil {
		t.Fatalf("expected Article=nil for parked article; got %+v", result.Article)
	}
	if result.LongPending == nil {
		t.Fatalf("expected LongPending != nil")
	}
	lp := result.LongPending
	if lp.PendingID == "" {
		t.Errorf("PendingID is empty")
	}
	if lp.Limit != limit {
		t.Errorf("Limit = %d, want %d", lp.Limit, limit)
	}
	if lp.Tokens <= limit {
		t.Errorf("Tokens = %d, must be > %d to qualify as too-long", lp.Tokens, limit)
	}
	if lp.Words <= 0 {
		t.Errorf("Words = %d, expected > 0 for the user-facing message", lp.Words)
	}

	if len(llmStub.AnalyzeCalls) != 0 {
		t.Errorf("LLM was called %d times — must not be invoked while parked", len(llmStub.AnalyzeCalls))
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("articles table got %d rows; parked articles must not be persisted", n)
	}
}

// TestAnalyzeArticle_BoundaryAtLimitPasses pins the boundary semantics:
// an article whose token estimate lands precisely on the limit goes through
// the normal pipeline (only strictly-greater values park).
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
	body := makeBody(limit * 4)
	if got := llm.EstimateTokens(body); got != limit {
		t.Fatalf("test fixture: EstimateTokens(body) = %d, want %d", got, limit)
	}

	llmStub := &mock.Provider{AnalyzeResp: sampleResponse()}
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

	result, err := svc.AnalyzeArticle(context.Background(), userID, "https://b", nil)
	if err != nil {
		t.Fatalf("boundary article must pass, got err: %v", err)
	}
	if result.LongPending != nil {
		t.Fatalf("boundary article must not park, got LongPending=%+v", result.LongPending)
	}
	if len(llmStub.AnalyzeCalls) != 1 {
		t.Errorf("expected LLM call=1 at boundary, got %d", len(llmStub.AnalyzeCalls))
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

	body := makeBody((articles.DefaultMaxTokensPerArticle + 100) * 4)

	llmStub := &mock.Provider{AnalyzeResp: sampleResponse()}
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

	result, err := svc.AnalyzeArticle(context.Background(), userID, "https://huge", nil)
	if err != nil {
		t.Fatalf("AnalyzeArticle: %v", err)
	}
	if result.LongPending == nil {
		t.Fatalf("expected LongPending under default limit, got Article=%+v", result.Article)
	}
	if len(llmStub.AnalyzeCalls) != 0 {
		t.Errorf("LLM must not be invoked while parked, calls=%d", len(llmStub.AnalyzeCalls))
	}
}

// TestAnalyzeExtracted_TruncateModeAnalyzesPrefix completes the loop:
// after a long article parks, AnalyzeExtracted with ModeTruncate drives
// the pipeline through to a stored article using a paragraph-prefix of the
// extracted body. The stored Notice carries the localized banner text.
func TestAnalyzeExtracted_TruncateModeAnalyzesPrefix(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)
	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)
	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	const limit = 50
	// Multi-paragraph body well above the limit.
	body := strings.Repeat("alpha beta gamma delta epsilon zeta eta theta\n\n", 30)

	llmStub := &mock.Provider{AnalyzeResp: sampleResponse()}
	svc := articles.NewService(articles.ServiceDeps{
		DB: db, Users: usersSvc, Languages: langs, Keys: keys,
		Extractor:    stubExtractor{out: articles.Extracted{URL: "https://t", URLHash: "ht", Title: "trunc", Content: body}},
		LLM:          llmStub,
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
		MaxTokens:    limit,
		Log:          slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})

	parked, err := svc.AnalyzeArticle(context.Background(), userID, "https://t", nil)
	if err != nil {
		t.Fatalf("AnalyzeArticle: %v", err)
	}
	if parked.LongPending == nil {
		t.Fatalf("expected parking, got Article=%+v", parked.Article)
	}

	notice := stubNotice{}
	analyzed, err := svc.AnalyzeExtracted(context.Background(), userID, parked.LongPending.PendingID, articles.ModeTruncate, notice, nil)
	if err != nil {
		t.Fatalf("AnalyzeExtracted: %v", err)
	}
	if analyzed.Stored == nil || analyzed.Stored.ID == 0 {
		t.Fatalf("expected stored article, got %+v", analyzed)
	}
	if analyzed.Notice == "" {
		t.Errorf("expected non-empty notice for truncate mode")
	}
	if len(llmStub.AnalyzeCalls) != 1 {
		t.Fatalf("expected exactly one LLM Analyze call, got %d", len(llmStub.AnalyzeCalls))
	}
	if got := llm.EstimateTokens(llmStub.AnalyzeCalls[0].ArticleText); got > limit {
		t.Errorf("truncated body sent to LLM is %d tokens, exceeds limit %d", got, limit)
	}
	if len(llmStub.SummarizeCalls) != 0 {
		t.Errorf("Summarize must not be invoked in truncate mode, calls=%d", len(llmStub.SummarizeCalls))
	}
}

// TestAnalyzeExtracted_SummarizeModeCallsLLMTwice covers the pre-summary
// fallback: AnalyzeExtracted first asks the LLM to compress the article,
// then runs the analysis on the summary. Both calls must fire, the analyze
// payload must be the summary (not the original), and the Notice must be
// non-empty.
func TestAnalyzeExtracted_SummarizeModeCallsLLMTwice(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)
	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)
	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	const limit = 50
	body := strings.Repeat("alpha beta gamma delta epsilon zeta eta theta\n\n", 30)

	llmStub := &mock.Provider{
		AnalyzeResp:   sampleResponse(),
		SummarizeResp: "summary of the article", // ≈ 5 tokens — well under the limit.
	}
	svc := articles.NewService(articles.ServiceDeps{
		DB: db, Users: usersSvc, Languages: langs, Keys: keys,
		Extractor:    stubExtractor{out: articles.Extracted{URL: "https://s", URLHash: "hs", Title: "sum", Content: body}},
		LLM:          llmStub,
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
		MaxTokens:    limit,
		Log:          slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})

	parked, err := svc.AnalyzeArticle(context.Background(), userID, "https://s", nil)
	if err != nil || parked.LongPending == nil {
		t.Fatalf("expected parking, got %+v err=%v", parked, err)
	}

	analyzed, err := svc.AnalyzeExtracted(context.Background(), userID, parked.LongPending.PendingID, articles.ModeSummarize, stubNotice{}, nil)
	if err != nil {
		t.Fatalf("AnalyzeExtracted: %v", err)
	}
	if analyzed.Stored == nil {
		t.Fatalf("expected stored article")
	}
	if analyzed.Notice == "" {
		t.Errorf("expected non-empty notice for summarize mode")
	}
	if len(llmStub.SummarizeCalls) != 1 {
		t.Fatalf("expected 1 Summarize call, got %d", len(llmStub.SummarizeCalls))
	}
	if len(llmStub.AnalyzeCalls) != 1 {
		t.Fatalf("expected 1 Analyze call, got %d", len(llmStub.AnalyzeCalls))
	}
	if got := llmStub.AnalyzeCalls[0].ArticleText; got != "summary of the article" {
		t.Errorf("Analyze should have run on the summary, got body=%q", got)
	}
}

// TestAnalyzeExtracted_ExpiredPendingReturnsTypedError pins the
// ErrPendingExpired contract: an unknown pending id surfaces a typed error
// the Telegram layer can map to the localized "session expired" message.
func TestAnalyzeExtracted_ExpiredPendingReturnsTypedError(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)
	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)
	userID := seedUserAndKey(t, db)

	svc := articles.NewService(articles.ServiceDeps{
		DB: db, Users: usersSvc, Languages: langs, Keys: keys,
		Extractor:    stubExtractor{},
		LLM:          &mock.Provider{},
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
	})

	_, err := svc.AnalyzeExtracted(context.Background(), userID, "no-such-id", articles.ModeTruncate, nil, nil)
	if !errors.Is(err, articles.ErrPendingExpired) {
		t.Fatalf("expected ErrPendingExpired, got %v", err)
	}
}

type stubNotice struct{}

func (stubNotice) RenderNotice(d articles.NoticeData) string {
	switch d.Kind {
	case articles.NoticeTruncated:
		return "TRUNCATED"
	case articles.NoticeSummarized:
		return "SUMMARIZED"
	default:
		return ""
	}
}
