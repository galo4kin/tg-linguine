package articles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("articles: not found")

// DBTX is the subset of *sql.DB / *sql.Tx that the repository needs. It lets
// callers compose Save with other repo operations inside a single transaction.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type Repository interface {
	// Insert persists a new article row using the supplied tx/db handle and writes
	// the resulting auto-increment id back to a.ID.
	Insert(ctx context.Context, q DBTX, a *Article) error
	// ByID fetches one article by primary key.
	ByID(ctx context.Context, q DBTX, id int64) (*Article, error)
	// CategoryIDByCode returns the categories.id for a code, inserting the row
	// if it does not yet exist.
	UpsertCategory(ctx context.Context, q DBTX, code string) (int64, error)
	// ListByUser returns up to `limit` articles for the user, ordered by
	// created_at DESC (newest first), starting at `offset`.
	ListByUser(ctx context.Context, q DBTX, userID int64, limit, offset int) ([]Article, error)
	// CountByUser returns how many articles the user has stored.
	CountByUser(ctx context.Context, q DBTX, userID int64) (int, error)
	// ByUserAndHash looks up an article previously stored for the user under the
	// same (url_hash, language_code) — returns ErrNotFound if no such row.
	ByUserAndHash(ctx context.Context, q DBTX, userID int64, urlHash, languageCode string) (*Article, error)
	// UpdateAdaptedVersions rewrites only the adapted_versions JSON blob for
	// an existing article — used by step 19 to merge a freshly generated
	// per-level adaptation without touching the rest of the row.
	UpdateAdaptedVersions(ctx context.Context, q DBTX, articleID int64, raw string) error
}

type sqliteRepo struct {
	db *sql.DB
}

func NewSQLiteRepository(db *sql.DB) Repository {
	return &sqliteRepo{db: db}
}

func (r *sqliteRepo) Insert(ctx context.Context, q DBTX, a *Article) error {
	const stmt = `
		INSERT INTO articles (
			user_id, source_url, source_url_hash, title, language_code,
			cefr_detected, summary_target, summary_native,
			adapted_versions, category_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	res, err := q.ExecContext(ctx, stmt,
		a.UserID,
		a.SourceURL,
		a.SourceURLHash,
		a.Title,
		a.LanguageCode,
		nullStr(a.CEFRDetected),
		nullStr(a.SummaryTarget),
		nullStr(a.SummaryNative),
		nullStr(a.AdaptedVersions),
		nullID(a.CategoryID),
	)
	if err != nil {
		return fmt.Errorf("articles: insert: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("articles: last id: %w", err)
	}
	a.ID = id
	return nil
}

func (r *sqliteRepo) ByID(ctx context.Context, q DBTX, id int64) (*Article, error) {
	const stmt = `
		SELECT id, user_id, source_url, source_url_hash, title, language_code,
		       COALESCE(cefr_detected, ''), COALESCE(summary_target, ''),
		       COALESCE(summary_native, ''), COALESCE(adapted_versions, ''),
		       COALESCE(category_id, 0), created_at
		FROM articles
		WHERE id = ?
	`
	var a Article
	err := q.QueryRowContext(ctx, stmt, id).Scan(
		&a.ID, &a.UserID, &a.SourceURL, &a.SourceURLHash, &a.Title, &a.LanguageCode,
		&a.CEFRDetected, &a.SummaryTarget, &a.SummaryNative, &a.AdaptedVersions,
		&a.CategoryID, &a.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("articles: byID: %w", err)
	}
	return &a, nil
}

func (r *sqliteRepo) ListByUser(ctx context.Context, q DBTX, userID int64, limit, offset int) ([]Article, error) {
	const stmt = `
		SELECT id, user_id, source_url, source_url_hash, title, language_code,
		       COALESCE(cefr_detected, ''), COALESCE(summary_target, ''),
		       COALESCE(summary_native, ''), COALESCE(adapted_versions, ''),
		       COALESCE(category_id, 0), created_at
		FROM articles
		WHERE user_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?
	`
	rows, err := q.QueryContext(ctx, stmt, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("articles: list by user: %w", err)
	}
	defer rows.Close()
	var out []Article
	for rows.Next() {
		var a Article
		if err := rows.Scan(
			&a.ID, &a.UserID, &a.SourceURL, &a.SourceURLHash, &a.Title, &a.LanguageCode,
			&a.CEFRDetected, &a.SummaryTarget, &a.SummaryNative, &a.AdaptedVersions,
			&a.CategoryID, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("articles: scan list: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("articles: iter list: %w", err)
	}
	return out, nil
}

func (r *sqliteRepo) CountByUser(ctx context.Context, q DBTX, userID int64) (int, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("articles: count by user: %w", err)
	}
	return n, nil
}

func (r *sqliteRepo) ByUserAndHash(ctx context.Context, q DBTX, userID int64, urlHash, languageCode string) (*Article, error) {
	const stmt = `
		SELECT id, user_id, source_url, source_url_hash, title, language_code,
		       COALESCE(cefr_detected, ''), COALESCE(summary_target, ''),
		       COALESCE(summary_native, ''), COALESCE(adapted_versions, ''),
		       COALESCE(category_id, 0), created_at
		FROM articles
		WHERE user_id = ? AND source_url_hash = ? AND language_code = ?
	`
	var a Article
	err := q.QueryRowContext(ctx, stmt, userID, urlHash, languageCode).Scan(
		&a.ID, &a.UserID, &a.SourceURL, &a.SourceURLHash, &a.Title, &a.LanguageCode,
		&a.CEFRDetected, &a.SummaryTarget, &a.SummaryNative, &a.AdaptedVersions,
		&a.CategoryID, &a.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("articles: by user and hash: %w", err)
	}
	return &a, nil
}

func (r *sqliteRepo) UpdateAdaptedVersions(ctx context.Context, q DBTX, articleID int64, raw string) error {
	if _, err := q.ExecContext(ctx, `UPDATE articles SET adapted_versions = ? WHERE id = ?`, nullStr(raw), articleID); err != nil {
		return fmt.Errorf("articles: update adapted: %w", err)
	}
	return nil
}

func (r *sqliteRepo) UpsertCategory(ctx context.Context, q DBTX, code string) (int64, error) {
	if code == "" {
		return 0, nil
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO categories (code) VALUES (?) ON CONFLICT(code) DO NOTHING`, code); err != nil {
		return 0, fmt.Errorf("articles: upsert category: %w", err)
	}
	var id int64
	if err := q.QueryRowContext(ctx, `SELECT id FROM categories WHERE code = ?`, code).Scan(&id); err != nil {
		return 0, fmt.Errorf("articles: select category: %w", err)
	}
	return id, nil
}

// WithTx is a free helper for callers that want to coordinate multiple repos
// inside the same transaction.
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("articles: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
		if err != nil {
			tx.Rollback()
			return
		}
		err = tx.Commit()
	}()
	err = fn(tx)
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
