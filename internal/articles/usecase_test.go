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

type stubLLM struct {
	resp        llm.AnalyzeResponse
	err         error
	lastRequest *llm.AnalyzeRequest
	calls       int
}

func (s *stubLLM) ValidateAPIKey(ctx context.Context, key string) error { return nil }
func (s *stubLLM) Analyze(ctx context.Context, key string, req llm.AnalyzeRequest) (llm.AnalyzeResponse, error) {
	s.calls++
	s.lastRequest = &req
	return s.resp, s.err
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
		LLM:          &stubLLM{resp: sampleResponse()},
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
	if result.Article.ID == 0 || len(result.Words) != 2 {
		t.Fatalf("unexpected: %+v", result)
	}
	if result.Article.CategoryID == 0 {
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
		LLM:          &stubLLM{},
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
		LLM:          &stubLLM{},
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
		LLM:          &stubLLM{err: llm.ErrRateLimited},
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

	llmStub := &stubLLM{resp: sampleResponse()}
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
	if extractorCalls != 1 || llmStub.calls != 1 {
		t.Fatalf("first: expected extractor=1 llm=1, got extractor=%d llm=%d", extractorCalls, llmStub.calls)
	}
	if first.Article.CEFRDetected != "B1" {
		t.Fatalf("expected stored CEFR=B1 (matches active), got %q", first.Article.CEFRDetected)
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
	if llmStub.calls != 1 {
		t.Fatalf("cache hit must not call llm; calls=%d", llmStub.calls)
	}
	if second.Article.ID != first.Article.ID {
		t.Fatalf("expected same article id, got %d vs %d", second.Article.ID, first.Article.ID)
	}
	if len(second.Words) != len(first.Words) {
		t.Fatalf("expected %d cached words, got %d", len(first.Words), len(second.Words))
	}
	for i := range first.Words {
		if first.Words[i].Lemma != second.Words[i].Lemma {
			t.Fatalf("word %d: lemma mismatch %q vs %q", i, first.Words[i].Lemma, second.Words[i].Lemma)
		}
	}

	// Sanity: only one article row was persisted.
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM articles WHERE user_id = ?`, userID).Scan(&n)
	if n != 1 {
		t.Fatalf("expected exactly 1 article row, got %d", n)
	}
}

// TestAnalyzeArticle_CacheMissOnCEFRChange covers the fall-through: when the
// stored article's detected CEFR differs from the user's current level, the
// pipeline must NOT shortcut and must invoke the extractor + LLM again. This
// pins the behavior that step 19 will revisit (regen on level change).
func TestAnalyzeArticle_CacheMissOnCEFRChange(t *testing.T) {
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

	llmStub := &stubLLM{err: llm.ErrUnavailable} // will be called and fail; counter is what we assert.
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

	_, err := svc.AnalyzeArticle(context.Background(), userID, rawURL, nil)
	if err == nil {
		t.Fatalf("expected error on extractor failure")
	}
	if extractorCalls != 1 {
		t.Fatalf("CEFR mismatch must fall through to extractor; calls=%d", extractorCalls)
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

	llmStub := &stubLLM{resp: sampleResponse()}
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
	if llmStub.lastRequest == nil || len(llmStub.lastRequest.KnownWords) != 0 {
		t.Fatalf("first call should send empty KnownWords, got %+v", llmStub.lastRequest)
	}

	// Mark "ipsum" as known and "lorem" as mastered for this user.
	if len(first.Words) != 2 {
		t.Fatalf("expected 2 stored words, got %d", len(first.Words))
	}
	if err := statuses.Upsert(context.Background(), db, dictionary.UserWordStatus{
		UserID: userID, DictionaryWordID: first.Words[0].ID, Status: dictionary.StatusKnown,
	}); err != nil {
		t.Fatalf("upsert known: %v", err)
	}
	if err := statuses.Upsert(context.Background(), db, dictionary.UserWordStatus{
		UserID: userID, DictionaryWordID: first.Words[1].ID, Status: dictionary.StatusMastered,
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
	if llmStub.lastRequest == nil {
		t.Fatalf("second call did not reach LLM")
	}
	got := llmStub.lastRequest.KnownWords
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
