package articles_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/crypto"
	"github.com/nikita/tg-linguine/internal/dictionary"
	"github.com/nikita/tg-linguine/internal/llm"
	"github.com/nikita/tg-linguine/internal/llm/mock"
	"github.com/nikita/tg-linguine/internal/storage"
	"github.com/nikita/tg-linguine/internal/users"
)

type stubExtractor struct {
	out articles.Extracted
	err error
}

func (s stubExtractor) Extract(ctx context.Context, url string) (articles.Extracted, error) {
	return s.out, s.err
}

func newServiceTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.RunMigrations(db, slog.New(slog.NewTextHandler(discardWriter{}, nil))); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedUserAndKey(t *testing.T, db *sql.DB) (userID int64) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO users (telegram_user_id, interface_language) VALUES (?, ?)`, 1001, "ru")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	id, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO user_languages (user_id, language_code, cefr_level, is_active) VALUES (?, ?, ?, 1)`, id, "en", "B1"); err != nil {
		t.Fatalf("seed lang: %v", err)
	}
	return id
}

func newCipher(t *testing.T) *crypto.AESGCM {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	rand.Read(key)
	c, err := crypto.New(key)
	if err != nil {
		t.Fatalf("crypto: %v", err)
	}
	return c
}

func sampleResponse() llm.AnalyzeResponse {
	return llm.AnalyzeResponse{
		SummaryTarget: "summary in target",
		SummaryNative: "краткое содержание",
		Category:      "fiction",
		CEFRDetected:  "B1",
		AdaptedVersions: llm.AdaptedVersions{
			Lower: "easy", Current: "current", Higher: "hard",
		},
		Words: []llm.AnalyzedWord{
			{SurfaceForm: "ipsum", Lemma: "ipsum", POS: "noun", TranscriptionIPA: "/ˈɪpsəm/", TranslationNative: "сам"},
			{SurfaceForm: "lorem", Lemma: "lorem", POS: "noun", TranscriptionIPA: "/ˈlɔːrəm/", TranslationNative: "тест"},
		},
		SafetyFlags: []string{},
	}
}

func TestAnalyzeArticle_HappyPath(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)

	usersRepo := users.NewSQLiteRepository(db)
	usersSvc := users.NewService(usersRepo)
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)

	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	svc := articles.NewService(articles.ServiceDeps{
		DB:           db,
		Users:        usersSvc,
		Languages:    langs,
		Keys:         keys,
		Extractor:    stubExtractor{out: articles.Extracted{URL: "https://x", NormalizedURL: "https://x", URLHash: "h", Title: "Title", Content: "body content body content", Lang: "en"}},
		LLM:          &mock.Provider{AnalyzeResp: sampleResponse()},
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
		Log:          slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})

	stages := []articles.Stage{}
	result, err := svc.AnalyzeArticle(context.Background(), userID, "https://x", func(s articles.Stage) {
		stages = append(stages, s)
	})
	if err != nil {
		t.Fatalf("AnalyzeArticle: %v", err)
	}
	if result.Article == nil || result.Article.Article.ID == 0 || len(result.Article.Words) != 2 {
		t.Fatalf("unexpected: %+v", result)
	}
	if result.Article.Article.CategoryID == 0 {
		t.Fatalf("expected category linked")
	}
	if len(stages) != 3 {
		t.Fatalf("expected 3 progress stages, got %d", len(stages))
	}

	// Check rows exist.
	var nA, nW, nAW, nS int
	db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&nA)
	db.QueryRow(`SELECT COUNT(*) FROM dictionary_words`).Scan(&nW)
	db.QueryRow(`SELECT COUNT(*) FROM article_words`).Scan(&nAW)
	db.QueryRow(`SELECT COUNT(*) FROM user_word_status`).Scan(&nS)
	if nA != 1 || nW != 2 || nAW != 2 || nS != 2 {
		t.Fatalf("counts: a=%d w=%d aw=%d s=%d", nA, nW, nAW, nS)
	}
}

func TestAnalyzeArticle_NoLanguage(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)
	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)

	res, err := db.Exec(`INSERT INTO users (telegram_user_id, interface_language) VALUES (?, ?)`, 9, "ru")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	userID, _ := res.LastInsertId()

	svc := articles.NewService(articles.ServiceDeps{
		DB: db, Users: usersSvc, Languages: langs, Keys: keys,
		Extractor:    stubExtractor{},
		LLM:          &mock.Provider{},
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
	})
	_, err = svc.AnalyzeArticle(context.Background(), userID, "https://x", nil)
	if !errors.Is(err, articles.ErrNoActiveLanguage) {
		t.Fatalf("expected ErrNoActiveLanguage, got %v", err)
	}
}

func TestAnalyzeArticle_NoAPIKey(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)
	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)
	userID := seedUserAndKey(t, db) // no key set

	svc := articles.NewService(articles.ServiceDeps{
		DB: db, Users: usersSvc, Languages: langs, Keys: keys,
		Extractor:    stubExtractor{},
		LLM:          &mock.Provider{},
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
	})
	_, err := svc.AnalyzeArticle(context.Background(), userID, "https://x", nil)
	if !errors.Is(err, articles.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
}

func TestAnalyzeArticle_LLMError(t *testing.T) {
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
		Extractor:    stubExtractor{out: articles.Extracted{URL: "https://x", URLHash: "h", Title: "t", Content: "c"}},
		LLM:          &mock.Provider{AnalyzeErr: llm.ErrRateLimited},
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
	})
	_, err := svc.AnalyzeArticle(context.Background(), userID, "https://x", nil)
	if !errors.Is(err, llm.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}

	// Nothing should have been persisted.
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&n)
	if n != 0 {
		t.Fatalf("articles persisted on LLM error: %d", n)
	}
}

// TestAnalyzeArticle_CacheHitOnSameUrlAndCEFR asserts the step-17 reuse flow:
// the second call on the same normalized URL + active language + matching
// CEFR must NOT touch the extractor or the LLM, and must return the stored
// article + the same word list (in original insertion order).
func TestAnalyzeArticle_CacheHitOnSameUrlAndCEFR(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)

	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)

	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	llmStub := &mock.Provider{AnalyzeResp: sampleResponse()}
	rawURL := "https://example.com/article?utm_source=x"
	normalized, _ := articles.NormalizeURL(rawURL)

	extractorCalls := 0
	wrappedExtractor := stubExtractorFn(func(ctx context.Context, url string) (articles.Extracted, error) {
		extractorCalls++
		return articles.Extracted{
			URL:           rawURL,
			NormalizedURL: normalized,
			URLHash:       articles.URLHash(normalized),
			Title:         "Cache me",
			Content:       "body content body content",
			Lang:          "en",
		}, nil
	})

	svc := articles.NewService(articles.ServiceDeps{
		DB:           db,
		Users:        usersSvc,
		Languages:    langs,
		Keys:         keys,
		Extractor:    wrappedExtractor,
		LLM:          llmStub,
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
		Log:          slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})

	first, err := svc.AnalyzeArticle(context.Background(), userID, rawURL, nil)
	if err != nil {
		t.Fatalf("first analyze: %v", err)
	}
	if extractorCalls != 1 || len(llmStub.AnalyzeCalls) != 1 {
		t.Fatalf("first: expected extractor=1 llm=1, got extractor=%d llm=%d", extractorCalls, len(llmStub.AnalyzeCalls))
	}
	if first.Article.Article.CEFRDetected != "B1" {
		t.Fatalf("expected stored CEFR=B1 (matches active), got %q", first.Article.Article.CEFRDetected)
	}

	// Same URL, second time. Even with different tracking params (utm_source) —
	// normalization should make the hash identical, and we expect a cache hit.
	second, err := svc.AnalyzeArticle(context.Background(), userID, rawURL+"&utm_campaign=y", nil)
	if err != nil {
		t.Fatalf("second analyze: %v", err)
	}
	if extractorCalls != 1 {
		t.Fatalf("cache hit must not call extractor; calls=%d", extractorCalls)
	}
	if len(llmStub.AnalyzeCalls) != 1 {
		t.Fatalf("cache hit must not call llm; calls=%d", len(llmStub.AnalyzeCalls))
	}
	if second.Article.Article.ID != first.Article.Article.ID {
		t.Fatalf("expected same article id, got %d vs %d", second.Article.Article.ID, first.Article.Article.ID)
	}
	if len(second.Article.Words) != len(first.Article.Words) {
		t.Fatalf("expected %d cached words, got %d", len(first.Article.Words), len(second.Article.Words))
	}
	for i := range first.Article.Words {
		if first.Article.Words[i].Lemma != second.Article.Words[i].Lemma {
			t.Fatalf("word %d: lemma mismatch %q vs %q", i, first.Article.Words[i].Lemma, second.Article.Words[i].Lemma)
		}
	}

	// Sanity: only one article row was persisted.
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM articles WHERE user_id = ?`, userID).Scan(&n)
	if n != 1 {
		t.Fatalf("expected exactly 1 article row, got %d", n)
	}
}

// TestAnalyzeArticle_CacheHitOnCEFRChange pins step 19's intent: when the
// user's CEFR has shifted since the article was originally analyzed, opening
// the same URL must still hit the cache (no extractor, no Analyze call). The
// per-level adaptation that the user is now missing is filled in lazily via
// Service.Adapt elsewhere — not by re-running the full pipeline.
func TestAnalyzeArticle_CacheHitOnCEFRChange(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)

	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)

	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	rawURL := "https://example.com/cefr-mismatch"
	normalized, _ := articles.NormalizeURL(rawURL)

	// Pre-seed an article with cefr_detected = B2 (user is at B1).
	repo := articles.NewSQLiteRepository(db)
	a := &articles.Article{
		UserID: userID, SourceURL: normalized, SourceURLHash: articles.URLHash(normalized),
		Title: "old", LanguageCode: "en", CEFRDetected: "B2",
	}
	if err := repo.Insert(context.Background(), db, a); err != nil {
		t.Fatalf("seed article: %v", err)
	}

	llmStub := &mock.Provider{AnalyzeErr: llm.ErrUnavailable}
	extractorCalls := 0
	svc := articles.NewService(articles.ServiceDeps{
		DB:        db,
		Users:     usersSvc,
		Languages: langs,
		Keys:      keys,
		Extractor: stubExtractorFn(func(ctx context.Context, url string) (articles.Extracted, error) {
			extractorCalls++
			return articles.Extracted{}, llm.ErrUnavailable
		}),
		LLM:          llmStub,
		Articles:     repo,
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
	})

	result, err := svc.AnalyzeArticle(context.Background(), userID, rawURL, nil)
	if err != nil {
		t.Fatalf("expected cache hit, got err: %v", err)
	}
	if extractorCalls != 0 {
		t.Fatalf("cache hit must not invoke extractor; calls=%d", extractorCalls)
	}
	if len(llmStub.AnalyzeCalls) != 0 {
		t.Fatalf("cache hit must not invoke LLM Analyze; calls=%d", len(llmStub.AnalyzeCalls))
	}
	if result.Article.Article.ID != a.ID {
		t.Fatalf("expected cached article id=%d, got %d", a.ID, result.Article.Article.ID)
	}
}

// TestAdapt_FillsMissingLevelAndCachesIt covers the full step 19 round trip:
// asking for a level that's missing from adapted_versions invokes the LLM
// mini-prompt, persists the result, and a second call for the same level is
// served from the merged JSON without touching the LLM again.
func TestAdapt_FillsMissingLevelAndCachesIt(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)

	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)

	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	repo := articles.NewSQLiteRepository(db)
	// Seed an article with a B1 adaptation already present (e.g. previously
	// analyzed when the user was at B1).
	a := &articles.Article{
		UserID:          userID,
		SourceURL:       "https://example.com/regen",
		SourceURLHash:   "h",
		Title:           "x",
		LanguageCode:    "en",
		CEFRDetected:    "B2",
		AdaptedVersions: `{"B1":"old b1 body"}`,
	}
	if err := repo.Insert(context.Background(), db, a); err != nil {
		t.Fatalf("seed: %v", err)
	}

	llmStub := &mock.Provider{
		AdaptResp: llm.AdaptResponse{AdaptedText: "fresh B2 body", SummaryTarget: "fresh summary"},
	}
	svc := articles.NewService(articles.ServiceDeps{
		DB:           db,
		Users:        usersSvc,
		Languages:    langs,
		Keys:         keys,
		Extractor:    stubExtractor{},
		LLM:          llmStub,
		Articles:     repo,
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     dictionary.NewSQLiteUserWordStatusRepository(db),
	})

	got, err := svc.Adapt(context.Background(), userID, a.ID, "B2")
	if err != nil {
		t.Fatalf("Adapt B2: %v", err)
	}
	if got != "fresh B2 body" {
		t.Fatalf("Adapt returned %q, want %q", got, "fresh B2 body")
	}
	if len(llmStub.AdaptCalls) != 1 {
		t.Fatalf("expected 1 LLM Adapt call, got %d", len(llmStub.AdaptCalls))
	}
	// The B1 source the LLM saw must be the previously stored adaptation.
	last := llmStub.AdaptCalls[len(llmStub.AdaptCalls)-1]
	if last.SourceText != "old b1 body" || last.SourceCEFR != "B1" {
		t.Fatalf("unexpected adapt request: %+v", last)
	}

	// The freshly generated B2 must now be merged into the JSON blob.
	stored, err := repo.ByID(context.Background(), db, a.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	parsed := stored.ParseAdaptedVersions()
	if parsed["B2"] != "fresh B2 body" || parsed["B1"] != "old b1 body" {
		t.Fatalf("merged adapted_versions wrong: %+v", parsed)
	}

	// Second call for the same level is a no-op cache hit — no extra LLM call.
	again, err := svc.Adapt(context.Background(), userID, a.ID, "B2")
	if err != nil {
		t.Fatalf("Adapt B2 (cache hit): %v", err)
	}
	if again != "fresh B2 body" {
		t.Fatalf("cache hit returned %q", again)
	}
	if len(llmStub.AdaptCalls) != 1 {
		t.Fatalf("cache hit must not invoke LLM Adapt; calls=%d", len(llmStub.AdaptCalls))
	}
}

type stubExtractorFn func(ctx context.Context, url string) (articles.Extracted, error)

func (f stubExtractorFn) Extract(ctx context.Context, url string) (articles.Extracted, error) {
	return f(ctx, url)
}

// TestAnalyzeArticle_KnownWordsForwarded asserts the integration of step 15:
// once the user has marked a word as `known`, a subsequent call to
// AnalyzeArticle for the same target language receives that lemma in
// KnownWords so the LLM can exclude it from the next analysis.
func TestAnalyzeArticle_KnownWordsForwarded(t *testing.T) {
	db := newServiceTestDB(t)
	cipher := newCipher(t)

	usersSvc := users.NewService(users.NewSQLiteRepository(db))
	langs := users.NewSQLiteUserLanguageRepository(db)
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)

	userID := seedUserAndKey(t, db)
	if err := keys.Set(context.Background(), userID, users.ProviderGroq, "gsk_test"); err != nil {
		t.Fatalf("set key: %v", err)
	}

	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)

	llmStub := &mock.Provider{AnalyzeResp: sampleResponse()}
	svc := articles.NewService(articles.ServiceDeps{
		DB:           db,
		Users:        usersSvc,
		Languages:    langs,
		Keys:         keys,
		Extractor:    stubExtractor{out: articles.Extracted{URL: "https://a", NormalizedURL: "https://a", URLHash: "ha", Title: "T1", Content: "body"}},
		LLM:          llmStub,
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     statuses,
		Log:          slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})

	first, err := svc.AnalyzeArticle(context.Background(), userID, "https://a", nil)
	if err != nil {
		t.Fatalf("first analyze: %v", err)
	}
	if len(llmStub.AnalyzeCalls) == 0 || len(llmStub.AnalyzeCalls[len(llmStub.AnalyzeCalls)-1].KnownWords) != 0 {
		t.Fatalf("first call should send empty KnownWords, got %+v", llmStub.AnalyzeCalls)
	}

	// Mark "ipsum" as known and "lorem" as mastered for this user.
	if len(first.Article.Words) != 2 {
		t.Fatalf("expected 2 stored words, got %d", len(first.Article.Words))
	}
	if err := statuses.Upsert(context.Background(), db, dictionary.UserWordStatus{
		UserID: userID, DictionaryWordID: first.Article.Words[0].ID, Status: dictionary.StatusKnown,
	}); err != nil {
		t.Fatalf("upsert known: %v", err)
	}
	if err := statuses.Upsert(context.Background(), db, dictionary.UserWordStatus{
		UserID: userID, DictionaryWordID: first.Article.Words[1].ID, Status: dictionary.StatusMastered,
	}); err != nil {
		t.Fatalf("upsert mastered: %v", err)
	}

	// Second analyze of a different URL — known lemmas must be forwarded.
	svc2 := articles.NewService(articles.ServiceDeps{
		DB:           db,
		Users:        usersSvc,
		Languages:    langs,
		Keys:         keys,
		Extractor:    stubExtractor{out: articles.Extracted{URL: "https://b", NormalizedURL: "https://b", URLHash: "hb", Title: "T2", Content: "other body"}},
		LLM:          llmStub,
		Articles:     articles.NewSQLiteRepository(db),
		Dictionary:   dictionary.NewSQLiteRepository(db),
		ArticleWords: dictionary.NewSQLiteArticleWordsRepository(db),
		Statuses:     statuses,
		Log:          slog.New(slog.NewTextHandler(discardWriter{}, nil)),
	})
	if _, err := svc2.AnalyzeArticle(context.Background(), userID, "https://b", nil); err != nil {
		t.Fatalf("second analyze: %v", err)
	}
	if len(llmStub.AnalyzeCalls) == 0 {
		t.Fatalf("second call did not reach LLM")
	}
	got := llmStub.AnalyzeCalls[len(llmStub.AnalyzeCalls)-1].KnownWords
	want := map[string]bool{"ipsum": true, "lorem": true}
	if len(got) != len(want) {
		t.Fatalf("KnownWords len: got %v want %v", got, want)
	}
	for _, lemma := range got {
		if !want[lemma] {
			t.Fatalf("unexpected known lemma %q (got=%v)", lemma, got)
		}
	}
}
