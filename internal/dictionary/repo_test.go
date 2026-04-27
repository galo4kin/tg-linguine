package dictionary_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/dictionary"
	"github.com/nikita/tg-linguine/internal/storage"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.RunMigrations(db, slog.New(slog.NewTextHandler(discardWriter{}, nil))); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (telegram_user_id, interface_language) VALUES (?, ?)`, 1001, "ru"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return db
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestDictionary_UpsertLemma_Dedup(t *testing.T) {
	db := newTestDB(t)
	repo := dictionary.NewSQLiteRepository(db)
	ctx := context.Background()

	id1, err := repo.UpsertLemma(ctx, db, dictionary.DictionaryWord{
		LanguageCode: "en", Lemma: "house", POS: "noun", TranscriptionIPA: "/haʊs/",
	})
	if err != nil {
		t.Fatalf("u1: %v", err)
	}
	id2, err := repo.UpsertLemma(ctx, db, dictionary.DictionaryWord{
		LanguageCode: "en", Lemma: "house",
	})
	if err != nil {
		t.Fatalf("u2: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected same id, got %d vs %d", id1, id2)
	}

	// Different language → different row.
	idDe, err := repo.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "de", Lemma: "house"})
	if err != nil {
		t.Fatalf("u3: %v", err)
	}
	if idDe == id1 {
		t.Fatalf("expected distinct id for different language_code")
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dictionary_words`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 dictionary rows, got %d", n)
	}
}

func TestDictionary_TransactionalAtomicity(t *testing.T) {
	db := newTestDB(t)
	artRepo := articles.NewSQLiteRepository(db)
	dict := dictionary.NewSQLiteRepository(db)
	awRepo := dictionary.NewSQLiteArticleWordsRepository(db)
	ctx := context.Background()

	wantErr := errors.New("mid-flight")
	err := articles.WithTx(ctx, db, func(tx *sql.Tx) error {
		a := &articles.Article{
			UserID: 1, SourceURL: "https://x", SourceURLHash: "h", Title: "t", LanguageCode: "en",
		}
		if err := artRepo.Insert(ctx, tx, a); err != nil {
			return err
		}
		wid, err := dict.UpsertLemma(ctx, tx, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "house"})
		if err != nil {
			return err
		}
		if err := awRepo.Insert(ctx, tx, dictionary.ArticleWord{
			ArticleID: a.ID, DictionaryWordID: wid, SurfaceForm: "houses",
		}); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}

	for _, tbl := range []string{"articles", "dictionary_words", "article_words"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		if n != 0 {
			t.Fatalf("rollback failed: %s has %d rows", tbl, n)
		}
	}
}

func TestUserWordStatus_Upsert(t *testing.T) {
	db := newTestDB(t)
	dict := dictionary.NewSQLiteRepository(db)
	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)
	ctx := context.Background()

	wid, err := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "tree"})
	if err != nil {
		t.Fatalf("dict: %v", err)
	}

	if err := statuses.Upsert(ctx, db, dictionary.UserWordStatus{
		UserID: 1, DictionaryWordID: wid, Status: dictionary.StatusLearning,
	}); err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	if err := statuses.Upsert(ctx, db, dictionary.UserWordStatus{
		UserID: 1, DictionaryWordID: wid, Status: dictionary.StatusKnown,
	}); err != nil {
		t.Fatalf("upsert2: %v", err)
	}

	got, err := statuses.Get(ctx, db, 1, wid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != dictionary.StatusKnown {
		t.Fatalf("expected known, got %s", got.Status)
	}
}
