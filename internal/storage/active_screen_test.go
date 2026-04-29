package storage_test

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/nikita/tg-linguine/internal/storage"
)

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

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
	return db
}

func TestActiveScreen(t *testing.T) {
	db := newTestDB(t)
	repo := storage.NewActiveScreenRepo(db)
	ctx := context.Background()

	const chatID int64 = 42

	t.Run("empty state - not found", func(t *testing.T) {
		_, _, _, found, err := repo.Get(ctx, chatID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if found {
			t.Fatal("expected not found")
		}
	})

	t.Run("set then get matches", func(t *testing.T) {
		if err := repo.Set(ctx, chatID, 100, "welcome", `{"foo":"bar"}`); err != nil {
			t.Fatalf("Set: %v", err)
		}

		msgID, screenID, ctxJSON, found, err := repo.Get(ctx, chatID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !found {
			t.Fatal("expected found")
		}
		if msgID != 100 {
			t.Errorf("messageID: got %d, want 100", msgID)
		}
		if screenID != "welcome" {
			t.Errorf("screenID: got %q, want %q", screenID, "welcome")
		}
		if ctxJSON != `{"foo":"bar"}` {
			t.Errorf("contextJSON: got %q, want %q", ctxJSON, `{"foo":"bar"}`)
		}
	})

	t.Run("overwrite shows new values", func(t *testing.T) {
		if err := repo.Set(ctx, chatID, 200, "mywords", `{}`); err != nil {
			t.Fatalf("Set: %v", err)
		}

		msgID, screenID, ctxJSON, found, err := repo.Get(ctx, chatID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !found {
			t.Fatal("expected found after overwrite")
		}
		if msgID != 200 {
			t.Errorf("messageID: got %d, want 200", msgID)
		}
		if screenID != "mywords" {
			t.Errorf("screenID: got %q, want %q", screenID, "mywords")
		}
		if ctxJSON != `{}` {
			t.Errorf("contextJSON: got %q, want %q", ctxJSON, `{}`)
		}
	})

	t.Run("clear then not found", func(t *testing.T) {
		if err := repo.Clear(ctx, chatID); err != nil {
			t.Fatalf("Clear: %v", err)
		}

		_, _, _, found, err := repo.Get(ctx, chatID)
		if err != nil {
			t.Fatalf("Get after clear: %v", err)
		}
		if found {
			t.Fatal("expected not found after clear")
		}
	})
}
