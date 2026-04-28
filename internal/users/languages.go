package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type UserLanguage struct {
	ID           int64
	UserID       int64
	LanguageCode string
	CEFRLevel    string
	IsActive     bool
	CreatedAt    time.Time
}

type UserLanguageRepository interface {
	Set(ctx context.Context, userID int64, languageCode, cefrLevel string) error
	Active(ctx context.Context, userID int64) (*UserLanguage, error)
	// List returns every language the user has ever picked, ordered by most
	// recently created first. Used by the settings menu to show the user's
	// learning roster alongside an "add new language" entry.
	List(ctx context.Context, userID int64) ([]UserLanguage, error)
	// Activate flips is_active to 1 for (userID, languageCode) and 0 for any
	// other rows the user has. Returns ErrNotFound if the user does not yet
	// have a row for that language — the caller is then expected to ask for
	// a CEFR level and call Set instead.
	Activate(ctx context.Context, userID int64, languageCode string) error
	// SetCEFR updates only the cefr_level for the user's currently active
	// language. Returns ErrNotFound when the user has no active language.
	SetCEFR(ctx context.Context, userID int64, cefrLevel string) error
}

type sqliteLanguageRepo struct {
	db *sql.DB
}

func NewSQLiteUserLanguageRepository(db *sql.DB) UserLanguageRepository {
	return &sqliteLanguageRepo{db: db}
}

func (r *sqliteLanguageRepo) Set(ctx context.Context, userID int64, languageCode, cefrLevel string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("user_languages: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE user_languages SET is_active = 0 WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("user_languages: deactivate: %w", err)
	}

	const upsert = `
		INSERT INTO user_languages (user_id, language_code, cefr_level, is_active)
		VALUES (?, ?, ?, 1)
		ON CONFLICT(user_id, language_code)
		DO UPDATE SET cefr_level = excluded.cefr_level, is_active = 1
	`
	if _, err := tx.ExecContext(ctx, upsert, userID, languageCode, cefrLevel); err != nil {
		return fmt.Errorf("user_languages: upsert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("user_languages: commit: %w", err)
	}
	return nil
}

func (r *sqliteLanguageRepo) List(ctx context.Context, userID int64) ([]UserLanguage, error) {
	const q = `
		SELECT id, user_id, language_code, cefr_level, is_active, created_at
		FROM user_languages
		WHERE user_id = ?
		ORDER BY created_at DESC, id DESC
	`
	rows, err := r.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("user_languages: list: %w", err)
	}
	defer rows.Close()
	var out []UserLanguage
	for rows.Next() {
		var ul UserLanguage
		var isActive int64
		if err := rows.Scan(&ul.ID, &ul.UserID, &ul.LanguageCode, &ul.CEFRLevel, &isActive, &ul.CreatedAt); err != nil {
			return nil, fmt.Errorf("user_languages: scan list: %w", err)
		}
		ul.IsActive = isActive != 0
		out = append(out, ul)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user_languages: iter list: %w", err)
	}
	return out, nil
}

func (r *sqliteLanguageRepo) Activate(ctx context.Context, userID int64, languageCode string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("user_languages: begin: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE user_languages SET is_active = 1 WHERE user_id = ? AND language_code = ?`,
		userID, languageCode,
	)
	if err != nil {
		return fmt.Errorf("user_languages: activate: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("user_languages: activate rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE user_languages SET is_active = 0 WHERE user_id = ? AND language_code != ?`,
		userID, languageCode,
	); err != nil {
		return fmt.Errorf("user_languages: deactivate others: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("user_languages: commit: %w", err)
	}
	return nil
}

func (r *sqliteLanguageRepo) SetCEFR(ctx context.Context, userID int64, cefrLevel string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE user_languages SET cefr_level = ? WHERE user_id = ? AND is_active = 1`,
		cefrLevel, userID,
	)
	if err != nil {
		return fmt.Errorf("user_languages: setCEFR: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("user_languages: setCEFR rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *sqliteLanguageRepo) Active(ctx context.Context, userID int64) (*UserLanguage, error) {
	const q = `
		SELECT id, user_id, language_code, cefr_level, is_active, created_at
		FROM user_languages
		WHERE user_id = ? AND is_active = 1
		LIMIT 1
	`
	var ul UserLanguage
	var isActive int64
	err := r.db.QueryRowContext(ctx, q, userID).Scan(
		&ul.ID, &ul.UserID, &ul.LanguageCode, &ul.CEFRLevel, &isActive, &ul.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user_languages: Active: %w", err)
	}
	ul.IsActive = isActive != 0
	return &ul, nil
}
