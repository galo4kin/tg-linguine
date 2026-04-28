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
}
