package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const ProviderGroq = "groq"

type cipherer interface {
	Encrypt(plain []byte) (cipher, nonce []byte, err error)
	Decrypt(cipher, nonce []byte) ([]byte, error)
}

type APIKeyRepository interface {
	Set(ctx context.Context, userID int64, provider, plain string) error
	Get(ctx context.Context, userID int64, provider string) (string, error)
}

type sqliteAPIKeyRepo struct {
	db     *sql.DB
	cipher cipherer
}

func NewSQLiteAPIKeyRepository(db *sql.DB, cipher cipherer) APIKeyRepository {
	return &sqliteAPIKeyRepo{db: db, cipher: cipher}
}

func (r *sqliteAPIKeyRepo) Set(ctx context.Context, userID int64, provider, plain string) error {
	ct, nonce, err := r.cipher.Encrypt([]byte(plain))
	if err != nil {
		return fmt.Errorf("api_keys: encrypt: %w", err)
	}
	const q = `
		INSERT INTO user_api_keys (user_id, provider, ciphertext, nonce)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, provider)
		DO UPDATE SET ciphertext = excluded.ciphertext, nonce = excluded.nonce
	`
	if _, err := r.db.ExecContext(ctx, q, userID, provider, ct, nonce); err != nil {
		return fmt.Errorf("api_keys: upsert: %w", err)
	}
	return nil
}

func (r *sqliteAPIKeyRepo) Get(ctx context.Context, userID int64, provider string) (string, error) {
	const q = `SELECT ciphertext, nonce FROM user_api_keys WHERE user_id = ? AND provider = ?`
	var ct, nonce []byte
	err := r.db.QueryRowContext(ctx, q, userID, provider).Scan(&ct, &nonce)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("api_keys: select: %w", err)
	}
	plain, err := r.cipher.Decrypt(ct, nonce)
	if err != nil {
		return "", fmt.Errorf("api_keys: decrypt: %w", err)
	}
	return string(plain), nil
}
