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
	resp llm.AnalyzeResponse
	err  error
}

func (s stubLLM) ValidateAPIKey(ctx context.Context, key string) error { return nil }
func (s stubLLM) Analyze(ctx context.Context, key string, req llm.AnalyzeRequest) (llm.AnalyzeResponse, error) {
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
		LLM:          stubLLM{resp: sampleResponse()},
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
		LLM:          stubLLM{},
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
		LLM:          stubLLM{},
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
		LLM:          stubLLM{err: llm.ErrRateLimited},
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
