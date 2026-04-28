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

func (r *sqliteRepo) ByID(ctx context.Context, id int64) (*User, error) {
	const q = `
		SELECT id, telegram_user_id, telegram_username, first_name,
		       interface_language, created_at, updated_at
		FROM users
		WHERE id = ?
	`
	var u User
	var username, firstName sql.NullString
	err := r.db.QueryRowContext(ctx, q, id).Scan(
		&u.ID, &u.TelegramUserID, &username, &firstName,
		&u.InterfaceLanguage, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("users: ByID: %w", err)
	}
	u.TelegramUsername = username.String
	u.FirstName = firstName.String
	return &u, nil
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

func (r *sqliteRepo) UpdateInterfaceLanguage(ctx context.Context, id int64, lang string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE users SET interface_language = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		lang, id,
	)
	if err != nil {
		return fmt.Errorf("users: UpdateInterfaceLanguage: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("users: UpdateInterfaceLanguage rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
