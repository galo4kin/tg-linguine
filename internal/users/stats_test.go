package users_test

import (
	"context"
	"testing"
	"time"

	"github.com/nikita/tg-linguine/internal/users"
)

// TestStats_TouchAndWindow seeds three users with manually-set last_seen_at
// (12h ago, 3d ago, 10d ago) and verifies Stats counts them in the right
// 24h / 7d windows. The freshly-inserted user is bumped via TouchLastSeen
// to confirm the write path works.
func TestStats_TouchAndWindow(t *testing.T) {
	db := newMigratedDB(t)
	repo := users.NewSQLiteRepository(db)
	ctx := context.Background()

	now := time.Now().UTC()
	uIDs := []int64{}
	for i, age := range []time.Duration{
		12 * time.Hour,
		3 * 24 * time.Hour,
		10 * 24 * time.Hour,
	} {
		u := &users.User{
			TelegramUserID:    int64(7000 + i),
			InterfaceLanguage: "en",
		}
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("create: %v", err)
		}
		// Force last_seen_at to a deterministic past timestamp; CURRENT_TIMESTAMP
		// default would otherwise cluster everyone in the 24h bucket.
		ts := now.Add(-age).Format("2006-01-02 15:04:05")
		if _, err := db.ExecContext(ctx,
			`UPDATE users SET last_seen_at = ? WHERE id = ?`, ts, u.ID,
		); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		uIDs = append(uIDs, u.ID)
	}

	// TouchLastSeen on the first user — it must come back into the 24h window.
	if err := repo.TouchLastSeen(ctx, 7000); err != nil {
		t.Fatalf("touch: %v", err)
	}

	s, err := repo.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if s.Total != 3 {
		t.Errorf("Total = %d, want 3", s.Total)
	}
	if s.Active24h != 1 {
		t.Errorf("Active24h = %d, want 1 (only freshly-touched user 7000)", s.Active24h)
	}
	if s.Active7d != 2 {
		t.Errorf("Active7d = %d, want 2 (touched user + 3-day-old)", s.Active7d)
	}
	_ = uIDs
}
