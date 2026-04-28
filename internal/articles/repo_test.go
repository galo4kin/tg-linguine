package articles_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"log/slog"

	"github.com/nikita/tg-linguine/internal/articles"
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
		t.Fatalf("seed user: %v", err)
	}
	return db
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestArticleRepository_InsertAndRead(t *testing.T) {
	db := newTestDB(t)
	repo := articles.NewSQLiteRepository(db)
	ctx := context.Background()

	a := &articles.Article{
		UserID:        1,
		SourceURL:     "https://example.com/a",
		SourceURLHash: "h1",
		Title:         "Example",
		LanguageCode:  "en",
		CEFRDetected:  "B1",
		SummaryTarget: "summary",
	}
	if err := repo.Insert(ctx, db, a); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if a.ID == 0 {
		t.Fatalf("expected id assigned")
	}

	got, err := repo.ByID(ctx, db, a.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Title != "Example" || got.CEFRDetected != "B1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestArticleRepository_UpsertCategory(t *testing.T) {
	db := newTestDB(t)
	repo := articles.NewSQLiteRepository(db)
	ctx := context.Background()

	id1, err := repo.UpsertCategory(ctx, db, "tech")
	if err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	id2, err := repo.UpsertCategory(ctx, db, "tech")
	if err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected same id on second upsert: %d vs %d", id1, id2)
	}

	if id, _ := repo.UpsertCategory(ctx, db, ""); id != 0 {
		t.Fatalf("empty code should return 0, got %d", id)
	}
}

func TestArticleRepository_ListByUser_Pagination(t *testing.T) {
	db := newTestDB(t)
	repo := articles.NewSQLiteRepository(db)
	ctx := context.Background()

	if _, err := db.Exec(`INSERT INTO users (telegram_user_id, interface_language) VALUES (?, ?)`, 1002, "en"); err != nil {
		t.Fatalf("seed user 2: %v", err)
	}

	// 12 articles for user 1, 3 for user 2.
	for i := 0; i < 12; i++ {
		a := &articles.Article{
			UserID:        1,
			SourceURL:     "https://example.com/u1-" + strconvI(i),
			SourceURLHash: "h1-" + strconvI(i),
			Title:         "U1 article " + strconvI(i),
			LanguageCode:  "en",
		}
		if err := repo.Insert(ctx, db, a); err != nil {
			t.Fatalf("insert u1: %v", err)
		}
	}
	for i := 0; i < 3; i++ {
		a := &articles.Article{
			UserID:        2,
			SourceURL:     "https://example.com/u2-" + strconvI(i),
			SourceURLHash: "h2-" + strconvI(i),
			Title:         "U2 article " + strconvI(i),
			LanguageCode:  "en",
		}
		if err := repo.Insert(ctx, db, a); err != nil {
			t.Fatalf("insert u2: %v", err)
		}
	}

	if n, err := repo.CountByUser(ctx, db, 1); err != nil || n != 12 {
		t.Fatalf("CountByUser(1) = %d, %v", n, err)
	}
	if n, err := repo.CountByUser(ctx, db, 2); err != nil || n != 3 {
		t.Fatalf("CountByUser(2) = %d, %v", n, err)
	}
	if n, err := repo.CountByUser(ctx, db, 999); err != nil || n != 0 {
		t.Fatalf("CountByUser(missing) = %d, %v", n, err)
	}

	page0, err := repo.ListByUser(ctx, db, 1, 10, 0)
	if err != nil {
		t.Fatalf("ListByUser p0: %v", err)
	}
	if len(page0) != 10 {
		t.Fatalf("expected 10 on page 0, got %d", len(page0))
	}
	page1, err := repo.ListByUser(ctx, db, 1, 10, 10)
	if err != nil {
		t.Fatalf("ListByUser p1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("expected 2 on page 1, got %d", len(page1))
	}

	// DESC order — within the same created_at second SQLite ties on id DESC.
	if page0[0].ID < page0[1].ID {
		t.Fatalf("expected newest-first ordering, got %d before %d", page0[0].ID, page0[1].ID)
	}

	for _, a := range page0 {
		if a.UserID != 1 {
			t.Fatalf("ListByUser(1) leaked user_id=%d row", a.UserID)
		}
	}
}

func strconvI(i int) string {
	return fmt.Sprintf("%03d", i)
}

func TestArticleRepository_UniqueConstraint(t *testing.T) {
	db := newTestDB(t)
	repo := articles.NewSQLiteRepository(db)
	ctx := context.Background()

	a := &articles.Article{
		UserID: 1, SourceURL: "https://x", SourceURLHash: "h", Title: "t", LanguageCode: "en",
	}
	if err := repo.Insert(ctx, db, a); err != nil {
		t.Fatalf("first: %v", err)
	}
	dup := &articles.Article{
		UserID: 1, SourceURL: "https://x", SourceURLHash: "h", Title: "t2", LanguageCode: "en",
	}
	err := repo.Insert(ctx, db, dup)
	if err == nil {
		t.Fatalf("expected unique constraint violation")
	}
}

func TestWithTx_RollbackOnError(t *testing.T) {
	db := newTestDB(t)
	repo := articles.NewSQLiteRepository(db)
	ctx := context.Background()

	wantErr := errors.New("boom")
	err := articles.WithTx(ctx, db, func(tx *sql.Tx) error {
		a := &articles.Article{
			UserID: 1, SourceURL: "https://example.com/x", SourceURLHash: "h-rollback",
			Title: "t", LanguageCode: "en",
		}
		if err := repo.Insert(ctx, tx, a); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rollback did not undo insert: %d rows", n)
	}
}

func TestWithTx_CommitOnSuccess(t *testing.T) {
	db := newTestDB(t)
	repo := articles.NewSQLiteRepository(db)
	ctx := context.Background()

	err := articles.WithTx(ctx, db, func(tx *sql.Tx) error {
		a := &articles.Article{
			UserID: 1, SourceURL: "https://example.com/x", SourceURLHash: "h-commit",
			Title: "t", LanguageCode: "en",
		}
		return repo.Insert(ctx, tx, a)
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("commit did not persist: %d rows", n)
	}
}
