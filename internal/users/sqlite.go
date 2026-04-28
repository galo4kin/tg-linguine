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

func (r *sqliteRepo) Delete(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("users: Delete begin: %w", err)
	}
	defer tx.Rollback()

	// Explicit deletes in FK-safe order. `article_words` cascades from
	// `articles.id`, so deleting `articles` is enough for that table.
	// `dictionary_words` is the shared lemma table — never touched here.
	stmts := [...]struct {
		name string
		sql  string
	}{
		{"user_word_status", `DELETE FROM user_word_status WHERE user_id = ?`},
		{"articles", `DELETE FROM articles WHERE user_id = ?`},
		{"user_api_keys", `DELETE FROM user_api_keys WHERE user_id = ?`},
		{"user_languages", `DELETE FROM user_languages WHERE user_id = ?`},
		{"users", `DELETE FROM users WHERE id = ?`},
	}
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s.sql, id); err != nil {
			return fmt.Errorf("users: Delete %s: %w", s.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("users: Delete commit: %w", err)
	}
	return nil
}

func (r *sqliteRepo) TouchLastSeen(ctx context.Context, tgID int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET last_seen_at = CURRENT_TIMESTAMP WHERE telegram_user_id = ?`,
		tgID,
	)
	if err != nil {
		return fmt.Errorf("users: TouchLastSeen: %w", err)
	}
	return nil
}

func (r *sqliteRepo) Stats(ctx context.Context) (Stats, error) {
	const q = `
		SELECT
		  COUNT(*) AS total,
		  SUM(CASE WHEN last_seen_at >= datetime('now', '-1 day')  THEN 1 ELSE 0 END) AS active24h,
		  SUM(CASE WHEN last_seen_at >= datetime('now', '-7 days') THEN 1 ELSE 0 END) AS active7d
		FROM users
	`
	var s Stats
	var a24, a7 sql.NullInt64
	if err := r.db.QueryRowContext(ctx, q).Scan(&s.Total, &a24, &a7); err != nil {
		return Stats{}, fmt.Errorf("users: Stats: %w", err)
	}
	s.Active24h = int(a24.Int64)
	s.Active7d = int(a7.Int64)
	return s, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
