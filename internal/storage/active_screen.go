package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ActiveScreenRepo persists the single "active screen" per chat.
type ActiveScreenRepo struct{ db *sql.DB }

// NewActiveScreenRepo returns a new ActiveScreenRepo backed by db.
func NewActiveScreenRepo(db *sql.DB) *ActiveScreenRepo {
	return &ActiveScreenRepo{db: db}
}

// Set upserts the active screen record for the given chat.
func (r *ActiveScreenRepo) Set(ctx context.Context, chatID int64, messageID int, screenID, contextJSON string) error {
	const q = `
INSERT INTO active_screen (chat_id, message_id, screen_id, context_json, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(chat_id) DO UPDATE SET
    message_id   = excluded.message_id,
    screen_id    = excluded.screen_id,
    context_json = excluded.context_json,
    updated_at   = excluded.updated_at`

	if _, err := r.db.ExecContext(ctx, q, chatID, messageID, screenID, contextJSON, time.Now().Unix()); err != nil {
		return fmt.Errorf("active_screen set: %w", err)
	}
	return nil
}

// Get retrieves the active screen for the given chat.
// found is false when no record exists.
func (r *ActiveScreenRepo) Get(ctx context.Context, chatID int64) (messageID int, screenID, contextJSON string, found bool, err error) {
	const q = `SELECT message_id, screen_id, context_json FROM active_screen WHERE chat_id = ?`

	row := r.db.QueryRowContext(ctx, q, chatID)
	if scanErr := row.Scan(&messageID, &screenID, &contextJSON); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return 0, "", "", false, nil
		}
		return 0, "", "", false, fmt.Errorf("active_screen get: %w", scanErr)
	}
	return messageID, screenID, contextJSON, true, nil
}

// Clear removes the active screen record for the given chat.
func (r *ActiveScreenRepo) Clear(ctx context.Context, chatID int64) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM active_screen WHERE chat_id = ?`, chatID); err != nil {
		return fmt.Errorf("active_screen clear: %w", err)
	}
	return nil
}
