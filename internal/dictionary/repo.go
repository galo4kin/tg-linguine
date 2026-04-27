package dictionary

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("dictionary: not found")

// DBTX is the subset of *sql.DB / *sql.Tx that dictionary repos need. It lets
// callers run all repos inside one transaction by passing a *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Repository owns the dictionary_words table.
type Repository interface {
	// UpsertLemma inserts a lemma if it does not already exist for the given
	// language and returns the dictionary_words.id either way. POS and IPA
	// are written only on first insert; subsequent calls do not overwrite them.
	UpsertLemma(ctx context.Context, q DBTX, w DictionaryWord) (int64, error)
	// ByID fetches a single dictionary word by primary key.
	ByID(ctx context.Context, q DBTX, id int64) (*DictionaryWord, error)
}

// ArticleWordsRepository owns the article_words join table.
type ArticleWordsRepository interface {
	// Insert writes one row of article_words. Caller is responsible for the
	// transaction boundary.
	Insert(ctx context.Context, q DBTX, w ArticleWord) error
}

// UserWordStatusRepository owns the user_word_status table.
type UserWordStatusRepository interface {
	// Upsert sets the status row for (user_id, dictionary_word_id). On insert
	// counters default to zero; on update only the status field is changed —
	// counters are preserved (they evolve via Increment*, added later).
	Upsert(ctx context.Context, q DBTX, s UserWordStatus) error
	// Get returns the status row, or ErrNotFound.
	Get(ctx context.Context, q DBTX, userID, wordID int64) (*UserWordStatus, error)
}

type sqliteRepo struct{ db *sql.DB }
type sqliteArticleWordsRepo struct{ db *sql.DB }
type sqliteStatusRepo struct{ db *sql.DB }

func NewSQLiteRepository(db *sql.DB) Repository                       { return &sqliteRepo{db} }
func NewSQLiteArticleWordsRepository(db *sql.DB) ArticleWordsRepository { return &sqliteArticleWordsRepo{db} }
func NewSQLiteUserWordStatusRepository(db *sql.DB) UserWordStatusRepository {
	return &sqliteStatusRepo{db}
}

func (r *sqliteRepo) UpsertLemma(ctx context.Context, q DBTX, w DictionaryWord) (int64, error) {
	const ins = `
		INSERT INTO dictionary_words (language_code, lemma, pos, transcription_ipa)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(language_code, lemma) DO NOTHING
	`
	if _, err := q.ExecContext(ctx, ins, w.LanguageCode, w.Lemma, nullStr(w.POS), nullStr(w.TranscriptionIPA)); err != nil {
		return 0, fmt.Errorf("dictionary: upsert lemma: %w", err)
	}
	var id int64
	if err := q.QueryRowContext(ctx, `SELECT id FROM dictionary_words WHERE language_code = ? AND lemma = ?`, w.LanguageCode, w.Lemma).Scan(&id); err != nil {
		return 0, fmt.Errorf("dictionary: select lemma id: %w", err)
	}
	return id, nil
}

func (r *sqliteRepo) ByID(ctx context.Context, q DBTX, id int64) (*DictionaryWord, error) {
	const stmt = `
		SELECT id, language_code, lemma,
		       COALESCE(pos, ''), COALESCE(transcription_ipa, '')
		FROM dictionary_words
		WHERE id = ?
	`
	var w DictionaryWord
	if err := q.QueryRowContext(ctx, stmt, id).Scan(&w.ID, &w.LanguageCode, &w.Lemma, &w.POS, &w.TranscriptionIPA); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dictionary: byID: %w", err)
	}
	return &w, nil
}

func (r *sqliteArticleWordsRepo) Insert(ctx context.Context, q DBTX, w ArticleWord) error {
	const stmt = `
		INSERT INTO article_words (
			article_id, dictionary_word_id, surface_form,
			translation_native, example_target, example_native
		) VALUES (?, ?, ?, ?, ?, ?)
	`
	if _, err := q.ExecContext(ctx, stmt,
		w.ArticleID, w.DictionaryWordID, w.SurfaceForm,
		nullStr(w.TranslationNative), nullStr(w.ExampleTarget), nullStr(w.ExampleNative),
	); err != nil {
		return fmt.Errorf("dictionary: insert article_word: %w", err)
	}
	return nil
}

func (r *sqliteStatusRepo) Upsert(ctx context.Context, q DBTX, s UserWordStatus) error {
	const stmt = `
		INSERT INTO user_word_status (user_id, dictionary_word_id, status, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id, dictionary_word_id)
		DO UPDATE SET status = excluded.status, updated_at = CURRENT_TIMESTAMP
	`
	if _, err := q.ExecContext(ctx, stmt, s.UserID, s.DictionaryWordID, string(s.Status)); err != nil {
		return fmt.Errorf("dictionary: upsert status: %w", err)
	}
	return nil
}

func (r *sqliteStatusRepo) Get(ctx context.Context, q DBTX, userID, wordID int64) (*UserWordStatus, error) {
	const stmt = `
		SELECT user_id, dictionary_word_id, status,
		       correct_streak, correct_total, wrong_total, updated_at
		FROM user_word_status
		WHERE user_id = ? AND dictionary_word_id = ?
	`
	var s UserWordStatus
	var status string
	if err := q.QueryRowContext(ctx, stmt, userID, wordID).Scan(
		&s.UserID, &s.DictionaryWordID, &status,
		&s.CorrectStreak, &s.CorrectTotal, &s.WrongTotal, &s.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("dictionary: status get: %w", err)
	}
	s.Status = WordStatus(status)
	return &s, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
