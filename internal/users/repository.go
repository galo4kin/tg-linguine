package users

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("user not found")

type Repository interface {
	ByID(ctx context.Context, id int64) (*User, error)
	ByTelegramID(ctx context.Context, tgID int64) (*User, error)
	Create(ctx context.Context, u *User) error
	UpdateInterfaceLanguage(ctx context.Context, id int64, lang string) error
	// Delete removes the user and all data scoped to that user (languages,
	// API keys, articles + their per-article words, word-statuses) in a
	// single transaction. The shared `dictionary_words` table is intentionally
	// left untouched.
	Delete(ctx context.Context, id int64) error
	// TouchLastSeen bumps `last_seen_at` to NOW for the given Telegram id.
	// Called on every incoming update from the user so /stats can answer
	// "active in last 24h / 7d". Missing user → silent no-op (avoids racing
	// with delete-then-resend).
	TouchLastSeen(ctx context.Context, tgID int64) error
	// Stats returns aggregate user counts for the admin /stats command.
	Stats(ctx context.Context) (Stats, error)
}

// Stats is the tally returned by Repository.Stats — used only by the admin
// /stats command. Active windows are counted against `last_seen_at`.
type Stats struct {
	Total     int
	Active24h int
	Active7d  int
}
