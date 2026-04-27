package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type sqliteRepo struct {
	db *sql.DB
}

func NewSQLiteRepository(db *sql.DB) Repository {
	return &sqliteRepo{db: db}
}

func (r *sqliteRepo) ByTelegramID(ctx context.Context, tgID int64) (*User, error) {
	const q = `
		SELECT id, telegram_user_id, telegram_username, first_name,
		       interface_language, created_at, updated_at
		FROM users
		WHERE telegram_user_id = ?
	`
	var u User
	var username, firstName sql.NullString
	err := r.db.QueryRowContext(ctx, q, tgID).Scan(
		&u.ID,
		&u.TelegramUserID,
		&username,
		&firstName,
		&u.InterfaceLanguage,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("users: ByTelegramID: %w", err)
	}
	u.TelegramUsername = username.String
	u.FirstName = firstName.String
	return &u, nil
}

func (r *sqliteRepo) Create(ctx context.Context, u *User) error {
	const q = `
		INSERT INTO users (telegram_user_id, telegram_username, first_name, interface_language)
		VALUES (?, ?, ?, ?)
	`
	res, err := r.db.ExecContext(ctx, q,
		u.TelegramUserID,
		nullString(u.TelegramUsername),
		nullString(u.FirstName),
		u.InterfaceLanguage,
	)
	if err != nil {
		return fmt.Errorf("users: Create: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("users: Create LastInsertId: %w", err)
	}
	u.ID = id
	return nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
