package users_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nikita/tg-linguine/internal/users"
)

// TestUserLanguageRepository_FullLifecycle exercises the full lifecycle of
// the per-user language roster: Set, List, Activate, SetCEFR, Active. The
// repo is a single transactional surface, so one big test catches the
// interactions between methods better than a fan-out of tiny ones.
func TestUserLanguageRepository_FullLifecycle(t *testing.T) {
	db := newMigratedDB(t)
	userID := insertUser(t, db, 1001, "ru")

	repo := users.NewSQLiteUserLanguageRepository(db)
	ctx := context.Background()

	// Empty list at the start.
	list, err := repo.List(ctx, userID)
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %v", list)
	}
	if _, err := repo.Active(ctx, userID); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("Active on fresh user: expected ErrNotFound, got %v", err)
	}
	if err := repo.SetCEFR(ctx, userID, "B1"); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("SetCEFR with no active language: expected ErrNotFound, got %v", err)
	}
	if err := repo.Activate(ctx, userID, "en"); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("Activate on missing row: expected ErrNotFound, got %v", err)
	}

	// First Set: creates an active row.
	if err := repo.Set(ctx, userID, "en", "B1"); err != nil {
		t.Fatalf("Set en B1: %v", err)
	}
	got, err := repo.Active(ctx, userID)
	if err != nil {
		t.Fatalf("Active after Set: %v", err)
	}
	if got.LanguageCode != "en" || got.CEFRLevel != "B1" || !got.IsActive {
		t.Fatalf("active row mismatch: %+v", got)
	}

	// Adding a second language deactivates the previous active row.
	if err := repo.Set(ctx, userID, "es", "A2"); err != nil {
		t.Fatalf("Set es A2: %v", err)
	}
	got, err = repo.Active(ctx, userID)
	if err != nil {
		t.Fatalf("Active after second Set: %v", err)
	}
	if got.LanguageCode != "es" {
		t.Fatalf("expected es active, got %q", got.LanguageCode)
	}

	// SetCEFR updates only the active language.
	if err := repo.SetCEFR(ctx, userID, "B2"); err != nil {
		t.Fatalf("SetCEFR: %v", err)
	}
	got, err = repo.Active(ctx, userID)
	if err != nil {
		t.Fatalf("Active after SetCEFR: %v", err)
	}
	if got.CEFRLevel != "B2" {
		t.Fatalf("CEFR mismatch: %q", got.CEFRLevel)
	}

	// Activate flips the active flag back to en, leaving es around at A2.
	if err := repo.Activate(ctx, userID, "en"); err != nil {
		t.Fatalf("Activate en: %v", err)
	}
	got, err = repo.Active(ctx, userID)
	if err != nil {
		t.Fatalf("Active after Activate en: %v", err)
	}
	if got.LanguageCode != "en" {
		t.Fatalf("expected en active, got %q", got.LanguageCode)
	}

	// List returns both rows; only one is active.
	list, err = repo.List(ctx, userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(list))
	}
	activeCount := 0
	for _, ul := range list {
		if ul.IsActive {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active row, got %d", activeCount)
	}
}

func TestUserRepository_CreateAndByTelegramID(t *testing.T) {
	db := newMigratedDB(t)
	repo := users.NewSQLiteRepository(db)
	ctx := context.Background()

	u := &users.User{
		TelegramUserID:    9090,
		TelegramUsername:  "bob",
		FirstName:         "Bob",
		InterfaceLanguage: "en",
	}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID == 0 {
		t.Fatalf("expected ID assigned")
	}

	got, err := repo.ByTelegramID(ctx, 9090)
	if err != nil {
		t.Fatalf("ByTelegramID: %v", err)
	}
	if got.ID != u.ID || got.TelegramUsername != "bob" || got.FirstName != "Bob" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	if _, err := repo.ByTelegramID(ctx, 1234567890); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing tg id, got %v", err)
	}
}

func TestUserRepository_UpdateInterfaceLanguage(t *testing.T) {
	db := newMigratedDB(t)
	repo := users.NewSQLiteRepository(db)
	ctx := context.Background()

	id := insertUser(t, db, 4242, "en")
	if err := repo.UpdateInterfaceLanguage(ctx, id, "ru"); err != nil {
		t.Fatalf("UpdateInterfaceLanguage: %v", err)
	}
	got, err := repo.ByID(ctx, id)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.InterfaceLanguage != "ru" {
		t.Fatalf("expected interface=ru, got %q", got.InterfaceLanguage)
	}

	if err := repo.UpdateInterfaceLanguage(ctx, 999, "en"); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing id, got %v", err)
	}
}

func TestUserService_SetInterfaceLanguageNormalizes(t *testing.T) {
	db := newMigratedDB(t)
	id := insertUser(t, db, 7000, "en")

	svc := users.NewService(users.NewSQLiteRepository(db))
	if err := svc.SetInterfaceLanguage(context.Background(), id, "ru-RU"); err != nil {
		t.Fatalf("SetInterfaceLanguage: %v", err)
	}
	got, _ := svc.ByID(context.Background(), id)
	if got.InterfaceLanguage != "ru" {
		t.Fatalf("expected normalized ru, got %q", got.InterfaceLanguage)
	}
	// Unknown locale falls back to "en".
	if err := svc.SetInterfaceLanguage(context.Background(), id, "fr-FR"); err != nil {
		t.Fatalf("SetInterfaceLanguage fr: %v", err)
	}
	got, _ = svc.ByID(context.Background(), id)
	if got.InterfaceLanguage != "en" {
		t.Fatalf("expected fallback en, got %q", got.InterfaceLanguage)
	}
}
