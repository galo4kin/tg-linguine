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
