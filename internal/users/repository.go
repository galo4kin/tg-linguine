package users

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("user not found")

type Repository interface {
	ByTelegramID(ctx context.Context, tgID int64) (*User, error)
	Create(ctx context.Context, u *User) error
}
