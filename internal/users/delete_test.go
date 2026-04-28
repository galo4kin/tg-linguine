package users_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/nikita/tg-linguine/internal/crypto"
	"github.com/nikita/tg-linguine/internal/storage"
	"github.com/nikita/tg-linguine/internal/users"
)

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// newMigratedDB spins up a real SQLite file (TempDir, auto-cleaned) and
// applies the embedded migrations so the FK CASCADEs covered by step 24
// match production exactly.
func newMigratedDB(t *testing.T) *sql.DB {
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
	return db
}

// seedDeletableUser populates a single user with rows in every table the
// /delete_me transaction is supposed to touch, plus a second user whose
// rows the wipe must not disturb.
func seedDeletableUser(t *testing.T, db *sql.DB) (victim int64, bystander int64) {
	t.Helper()
	ctx := context.Background()

	victim = insertUser(t, db, 1001, "ru")
	bystander = insertUser(t, db, 2002, "en")

	// languages
	if _, err := db.ExecContext(ctx, `INSERT INTO user_languages (user_id, language_code, cefr_level) VALUES (?, 'en', 'B1')`, victim); err != nil {
		t.Fatalf("seed lang victim: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO user_languages (user_id, language_code, cefr_level) VALUES (?, 'es', 'A2')`, bystander); err != nil {
		t.Fatalf("seed lang bystander: %v", err)
	}

	// API keys (raw bytes are fine for this test — we only count rows).
	if _, err := db.ExecContext(ctx, `INSERT INTO user_api_keys (user_id, provider, ciphertext, nonce) VALUES (?, 'groq', X'00', X'00')`, victim); err != nil {
		t.Fatalf("seed key victim: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO user_api_keys (user_id, provider, ciphertext, nonce) VALUES (?, 'groq', X'00', X'00')`, bystander); err != nil {
		t.Fatalf("seed key bystander: %v", err)
	}

	// Two articles for victim, one for bystander; each gets one article_word.
	victimArticle := insertArticle(t, db, victim, "https://x/v", "vh1", "en")
	insertArticle(t, db, victim, "https://x/v2", "vh2", "en")
	bystanderArticle := insertArticle(t, db, bystander, "https://x/b", "bh", "en")

	// Shared dictionary lemma — must survive the wipe.
	var lemmaID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO dictionary_words (language_code, lemma) VALUES ('en', 'serendipity') RETURNING id`,
	).Scan(&lemmaID); err != nil {
		t.Fatalf("seed lemma: %v", err)
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO article_words (article_id, dictionary_word_id, surface_form, translation_native)
		 VALUES (?, ?, 'serendipity', 'случайность')`,
		victimArticle, lemmaID,
	); err != nil {
		t.Fatalf("seed article_words victim: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO article_words (article_id, dictionary_word_id, surface_form, translation_native)
		 VALUES (?, ?, 'serendipity', 'casualidad')`,
		bystanderArticle, lemmaID,
	); err != nil {
		t.Fatalf("seed article_words bystander: %v", err)
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO user_word_status (user_id, dictionary_word_id, status) VALUES (?, ?, 'learning')`,
		victim, lemmaID,
	); err != nil {
		t.Fatalf("seed status victim: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO user_word_status (user_id, dictionary_word_id, status) VALUES (?, ?, 'known')`,
		bystander, lemmaID,
	); err != nil {
		t.Fatalf("seed status bystander: %v", err)
	}

	return victim, bystander
}

func insertUser(t *testing.T, db *sql.DB, tgID int64, lang string) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO users (telegram_user_id, interface_language) VALUES (?, ?)`,
		tgID, lang,
	)
	if err != nil {
		t.Fatalf("insert user %d: %v", tgID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return id
}

func insertArticle(t *testing.T, db *sql.DB, userID int64, url, hash, lang string) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO articles (user_id, source_url, source_url_hash, title, language_code) VALUES (?, ?, ?, 'title', ?)`,
		userID, url, hash, lang,
	)
	if err != nil {
		t.Fatalf("insert article: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return id
}

func count(t *testing.T, db *sql.DB, sqlText string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(sqlText, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", sqlText, err)
	}
	return n
}

func TestUserRepository_Delete_WipesAllUserDataKeepsDictionary(t *testing.T) {
	db := newMigratedDB(t)
	victim, bystander := seedDeletableUser(t, db)

	repo := users.NewSQLiteRepository(db)
	if err := repo.Delete(context.Background(), victim); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// No rows for the victim anywhere.
	for _, q := range []struct {
		name string
		sql  string
	}{
		{"users", `SELECT COUNT(*) FROM users WHERE id = ?`},
		{"user_languages", `SELECT COUNT(*) FROM user_languages WHERE user_id = ?`},
		{"user_api_keys", `SELECT COUNT(*) FROM user_api_keys WHERE user_id = ?`},
		{"articles", `SELECT COUNT(*) FROM articles WHERE user_id = ?`},
		{"user_word_status", `SELECT COUNT(*) FROM user_word_status WHERE user_id = ?`},
	} {
		if n := count(t, db, q.sql, victim); n != 0 {
			t.Errorf("%s: expected 0 rows for victim, got %d", q.name, n)
		}
	}

	// article_words must cascade out via articles.id ON DELETE CASCADE.
	if n := count(t, db,
		`SELECT COUNT(*) FROM article_words aw
		 JOIN articles a ON a.id = aw.article_id
		 WHERE a.user_id = ?`, victim); n != 0 {
		t.Errorf("article_words: expected 0 rows under victim's articles, got %d", n)
	}

	// Bystander rows are intact.
	if n := count(t, db, `SELECT COUNT(*) FROM users WHERE id = ?`, bystander); n != 1 {
		t.Errorf("bystander user: expected 1 row, got %d", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM user_languages WHERE user_id = ?`, bystander); n != 1 {
		t.Errorf("bystander languages: expected 1 row, got %d", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM user_api_keys WHERE user_id = ?`, bystander); n != 1 {
		t.Errorf("bystander api keys: expected 1 row, got %d", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM articles WHERE user_id = ?`, bystander); n != 1 {
		t.Errorf("bystander articles: expected 1 row, got %d", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM user_word_status WHERE user_id = ?`, bystander); n != 1 {
		t.Errorf("bystander word status: expected 1 row, got %d", n)
	}

	// dictionary_words is shared and must NOT shrink.
	if n := count(t, db, `SELECT COUNT(*) FROM dictionary_words`); n != 1 {
		t.Errorf("dictionary_words must not shrink, got %d rows", n)
	}
}

func TestUserRepository_Delete_NonexistentUserDoesNotError(t *testing.T) {
	db := newMigratedDB(t)
	repo := users.NewSQLiteRepository(db)

	// No row with id=999 — Delete should be a no-op, not an error. This keeps
	// the /delete_me handler simple (it cannot accidentally surface a confusing
	// error if the user already pressed the button twice).
	if err := repo.Delete(context.Background(), 999); err != nil {
		t.Fatalf("Delete on missing id: %v", err)
	}
}

func TestUserService_DeleteUser_DelegatesToRepository(t *testing.T) {
	db := newMigratedDB(t)
	id := insertUser(t, db, 4242, "en")

	svc := users.NewService(users.NewSQLiteRepository(db))
	if err := svc.DeleteUser(context.Background(), id); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := svc.ByID(context.Background(), id); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after DeleteUser, got %v", err)
	}
}

// Sanity check that the encryption helper still functions when wired through
// SQLite — this gives us confidence the test harness mirrors the prod stack.
func TestNewMigratedDB_EncryptionRoundtrip(t *testing.T) {
	db := newMigratedDB(t)
	id := insertUser(t, db, 7777, "en")

	key := make([]byte, crypto.KeySize)
	rand.Read(key)
	cipher, err := crypto.New(key)
	if err != nil {
		t.Fatalf("crypto: %v", err)
	}
	keys := users.NewSQLiteAPIKeyRepository(db, cipher)
	if err := keys.Set(context.Background(), id, users.ProviderGroq, "gsk_xx"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := keys.Get(context.Background(), id, users.ProviderGroq)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "gsk_xx" {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}
