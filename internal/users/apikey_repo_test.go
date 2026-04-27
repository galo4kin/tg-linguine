package users_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/nikita/tg-linguine/internal/crypto"
	"github.com/nikita/tg-linguine/internal/users"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	const schema = `
CREATE TABLE users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  telegram_user_id INTEGER NOT NULL UNIQUE
);
CREATE TABLE user_api_keys (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  provider TEXT NOT NULL,
  ciphertext BLOB NOT NULL,
  nonce BLOB NOT NULL,
  UNIQUE(user_id, provider)
);
INSERT INTO users (id, telegram_user_id) VALUES (1, 1001);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

func TestAPIKeyRepository_Roundtrip(t *testing.T) {
	db := newTestDB(t)

	key := make([]byte, crypto.KeySize)
	rand.Read(key)
	cipher, err := crypto.New(key)
	if err != nil {
		t.Fatalf("crypto: %v", err)
	}

	repo := users.NewSQLiteAPIKeyRepository(db, cipher)
	ctx := context.Background()
	const plain = "gsk_super_secret_value_42"

	if err := repo.Set(ctx, 1, users.ProviderGroq, plain); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := repo.Get(ctx, 1, users.ProviderGroq)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != plain {
		t.Fatalf("round-trip mismatch: %q vs %q", got, plain)
	}

	// Confirm no plaintext in the stored row.
	var ct []byte
	if err := db.QueryRow(`SELECT ciphertext FROM user_api_keys WHERE user_id = 1`).Scan(&ct); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if string(ct) == plain {
		t.Fatalf("ciphertext column must not equal plaintext")
	}
}

func TestAPIKeyRepository_NotFound(t *testing.T) {
	db := newTestDB(t)
	key := make([]byte, crypto.KeySize)
	rand.Read(key)
	cipher, _ := crypto.New(key)
	repo := users.NewSQLiteAPIKeyRepository(db, cipher)

	_, err := repo.Get(context.Background(), 1, users.ProviderGroq)
	if !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAPIKeyRepository_Upsert(t *testing.T) {
	db := newTestDB(t)
	key := make([]byte, crypto.KeySize)
	rand.Read(key)
	cipher, _ := crypto.New(key)
	repo := users.NewSQLiteAPIKeyRepository(db, cipher)
	ctx := context.Background()

	if err := repo.Set(ctx, 1, users.ProviderGroq, "first"); err != nil {
		t.Fatalf("set1: %v", err)
	}
	if err := repo.Set(ctx, 1, users.ProviderGroq, "second"); err != nil {
		t.Fatalf("set2: %v", err)
	}
	got, err := repo.Get(ctx, 1, users.ProviderGroq)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "second" {
		t.Fatalf("expected updated value, got %q", got)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_api_keys WHERE user_id = 1`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 row after upsert, got %d", n)
	}
}
