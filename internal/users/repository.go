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
}
