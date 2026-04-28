package dictionary

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
	// CountAll returns the total number of distinct lemmas across all users
	// and languages — used by admin /stats.
	CountAll(ctx context.Context, q DBTX) (int, error)
}

// ArticleWordsRepository owns the article_words join table.
type ArticleWordsRepository interface {
	// Insert writes one row of article_words. Caller is responsible for the
	// transaction boundary.
	Insert(ctx context.Context, q DBTX, w ArticleWord) error
	// CountByArticle returns how many word entries are stored for an article.
	CountByArticle(ctx context.Context, q DBTX, articleID int64) (int, error)
	// PageByArticle returns up to `limit` word rows for the article, ordered
	// by article_words.rowid (i.e. insertion order), starting at `offset`.
	PageByArticle(ctx context.Context, q DBTX, articleID int64, limit, offset int) ([]ArticleWordView, error)
}

// ArticleWordView is the join of article_words ⨝ dictionary_words used for
// rendering the words list to the user.
type ArticleWordView struct {
	DictionaryWordID  int64
	SurfaceForm       string
	Lemma             string
	POS               string
	TranscriptionIPA  string
	TranslationNative string
	ExampleTarget     string
	ExampleNative     string
}

// UserWordStatusRepository owns the user_word_status table.
type UserWordStatusRepository interface {
	// Upsert sets the status row for (user_id, dictionary_word_id). On insert
	// counters default to zero; on update only the status field is changed —
	// counters are preserved (they evolve via Increment*, added later).
	Upsert(ctx context.Context, q DBTX, s UserWordStatus) error
	// Get returns the status row, or ErrNotFound.
	Get(ctx context.Context, q DBTX, userID, wordID int64) (*UserWordStatus, error)
	// GetMany returns the per-word status for the given user, keyed by
	// dictionary_word_id. Words without a status row are absent from the map
	// (the caller should treat them as the implicit default).
	GetMany(ctx context.Context, q DBTX, userID int64, wordIDs []int64) (map[int64]WordStatus, error)
	// KnownLemmas returns the lemmas of words the user has marked as known
	// or mastered for a given target language, sorted alphabetically. These
	// lemmas are passed to the LLM so it skips re-introducing them.
	KnownLemmas(ctx context.Context, q DBTX, userID int64, languageCode string) ([]string, error)
	// CountUserWords returns the number of vocabulary entries the user has
	// for a target language, optionally restricted to a subset of statuses
	// (passing nil counts every status row).
	CountUserWords(ctx context.Context, q DBTX, userID int64, languageCode string, statuses []WordStatus) (int, error)
	// PageUserWords returns up to `limit` vocabulary entries starting at
	// `offset`, ordered alphabetically by lemma. Same status semantics as
	// CountUserWords.
	PageUserWords(ctx context.Context, q DBTX, userID int64, languageCode string, statuses []WordStatus, limit, offset int) ([]UserWordEntry, error)
	// LearningQueue returns up to `limit` words the user is currently
	// learning in the given language, oldest `updated_at` first so cards
	// the user has not seen recently are surfaced before fresh ones.
	LearningQueue(ctx context.Context, q DBTX, userID int64, languageCode string, limit int) ([]LearningEntry, error)
	// SampleArticleWords returns one representative article_words row per
	// requested word id (the most recent occurrence by article id), keyed
	// by dictionary_word_id. Words with no article occurrence are absent.
	SampleArticleWords(ctx context.Context, q DBTX, wordIDs []int64) (map[int64]ArticleWordSample, error)
	// RecordCorrect increments the (user, word) pair's correct counters and
	// promotes the row to `mastered` once correct_streak reaches threshold.
	// Returns the post-update streak and whether the status flipped this
	// call.
	RecordCorrect(ctx context.Context, q DBTX, userID, wordID int64, threshold int) (newStreak int, mastered bool, err error)
	// RecordWrong resets correct_streak to 0 and increments wrong_total.
	RecordWrong(ctx context.Context, q DBTX, userID, wordID int64) error
	// DeleteWordStatus removes the user's status row for the given word.
	// Deleting a row that does not exist is not an error.
	DeleteWordStatus(ctx context.Context, q DBTX, userID, wordID int64) error
	// SampleDistractors returns up to `n` unique strings to use as wrong-answer
	// options in a quiz card. Direction picks the answer space:
	// DistractorForeignToNative draws native translations from article_words,
	// DistractorNativeToForeign draws foreign lemmas from dictionary_words.
	// The user's own vocabulary is preferred so options feel familiar; if it
	// cannot fill the quota, the global pool for `languageCode` backfills.
	// `correctAnswer` and `excludeWordID` are filtered out (case-insensitive).
	// Returns fewer than `n` only when the database has too few candidates —
	// the caller decides whether to proceed or skip the card.
	SampleDistractors(ctx context.Context, q DBTX, userID int64, languageCode string, excludeWordID int64, correctAnswer string, direction DistractorDirection, n int) ([]string, error)
}

// DistractorDirection picks which column SampleDistractors draws from.
type DistractorDirection string

const (
	// DistractorForeignToNative — quiz prompt is the foreign lemma and the
	// answer space is native translations (article_words.translation_native).
	DistractorForeignToNative DistractorDirection = "fwd"
	// DistractorNativeToForeign — quiz prompt is the native translation and
	// the answer space is foreign lemmas (dictionary_words.lemma).
	DistractorNativeToForeign DistractorDirection = "bwd"
)

// LearningEntry is the minimal projection used to assemble a study deck.
// Article-side fields (surface form, examples) come from
// SampleArticleWords so the queue query stays cheap.
type LearningEntry struct {
	DictionaryWordID int64
	Lemma            string
	POS              string
	TranscriptionIPA string
	CorrectStreak    int
}

// ArticleWordSample is one representative article_words row used to
// decorate a study card with its surface form and example sentences.
type ArticleWordSample struct {
	DictionaryWordID  int64
	SurfaceForm       string
	TranslationNative string
	ExampleTarget     string
	ExampleNative     string
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

func (r *sqliteRepo) CountAll(ctx context.Context, q DBTX) (int, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM dictionary_words`).Scan(&n); err != nil {
		return 0, fmt.Errorf("dictionary: count all: %w", err)
	}
	return n, nil
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

func (r *sqliteArticleWordsRepo) CountByArticle(ctx context.Context, q DBTX, articleID int64) (int, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM article_words WHERE article_id = ?`, articleID).Scan(&n); err != nil {
		return 0, fmt.Errorf("dictionary: count article_words: %w", err)
	}
	return n, nil
}

func (r *sqliteArticleWordsRepo) PageByArticle(ctx context.Context, q DBTX, articleID int64, limit, offset int) ([]ArticleWordView, error) {
	const stmt = `
		SELECT aw.dictionary_word_id, aw.surface_form,
		       dw.lemma, COALESCE(dw.pos, ''), COALESCE(dw.transcription_ipa, ''),
		       COALESCE(aw.translation_native, ''),
		       COALESCE(aw.example_target, ''), COALESCE(aw.example_native, '')
		FROM article_words aw
		JOIN dictionary_words dw ON dw.id = aw.dictionary_word_id
		WHERE aw.article_id = ?
		ORDER BY aw.rowid
		LIMIT ? OFFSET ?
	`
	rows, err := q.QueryContext(ctx, stmt, articleID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("dictionary: page article_words: %w", err)
	}
	defer rows.Close()

	var out []ArticleWordView
	for rows.Next() {
		var v ArticleWordView
		if err := rows.Scan(
			&v.DictionaryWordID, &v.SurfaceForm,
			&v.Lemma, &v.POS, &v.TranscriptionIPA,
			&v.TranslationNative, &v.ExampleTarget, &v.ExampleNative,
		); err != nil {
			return nil, fmt.Errorf("dictionary: scan article_word view: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dictionary: iter article_words: %w", err)
	}
	return out, nil
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

func (r *sqliteStatusRepo) GetMany(ctx context.Context, q DBTX, userID int64, wordIDs []int64) (map[int64]WordStatus, error) {
	out := make(map[int64]WordStatus, len(wordIDs))
	if len(wordIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(wordIDs))
	args := make([]any, 0, len(wordIDs)+1)
	args = append(args, userID)
	for i, id := range wordIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	stmt := `
		SELECT dictionary_word_id, status
		FROM user_word_status
		WHERE user_id = ? AND dictionary_word_id IN (` + strings.Join(placeholders, ",") + `)
	`
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("dictionary: status get many: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, fmt.Errorf("dictionary: scan status: %w", err)
		}
		out[id] = WordStatus(status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dictionary: iter statuses: %w", err)
	}
	return out, nil
}

func (r *sqliteStatusRepo) KnownLemmas(ctx context.Context, q DBTX, userID int64, languageCode string) ([]string, error) {
	const stmt = `
		SELECT dw.lemma
		FROM user_word_status uws
		JOIN dictionary_words dw ON dw.id = uws.dictionary_word_id
		WHERE uws.user_id = ?
		  AND dw.language_code = ?
		  AND uws.status IN ('known', 'mastered')
		ORDER BY dw.lemma
	`
	rows, err := q.QueryContext(ctx, stmt, userID, languageCode)
	if err != nil {
		return nil, fmt.Errorf("dictionary: known lemmas: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var lemma string
		if err := rows.Scan(&lemma); err != nil {
			return nil, fmt.Errorf("dictionary: scan known lemma: %w", err)
		}
		out = append(out, lemma)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dictionary: iter known lemmas: %w", err)
	}
	return out, nil
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

func (r *sqliteStatusRepo) CountUserWords(ctx context.Context, q DBTX, userID int64, languageCode string, statuses []WordStatus) (int, error) {
	stmt, args := buildUserWordsQuery(
		`SELECT COUNT(*)
		 FROM user_word_status uws
		 JOIN dictionary_words dw ON dw.id = uws.dictionary_word_id
		 WHERE uws.user_id = ? AND dw.language_code = ?`,
		"", userID, languageCode, statuses, 0, 0,
	)
	var n int
	if err := q.QueryRowContext(ctx, stmt, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("dictionary: count user words: %w", err)
	}
	return n, nil
}

func (r *sqliteStatusRepo) PageUserWords(ctx context.Context, q DBTX, userID int64, languageCode string, statuses []WordStatus, limit, offset int) ([]UserWordEntry, error) {
	stmt, args := buildUserWordsQuery(
		`SELECT dw.id, dw.lemma, COALESCE(dw.pos, ''), uws.status
		 FROM user_word_status uws
		 JOIN dictionary_words dw ON dw.id = uws.dictionary_word_id
		 WHERE uws.user_id = ? AND dw.language_code = ?`,
		` ORDER BY dw.lemma ASC LIMIT ? OFFSET ?`,
		userID, languageCode, statuses, limit, offset,
	)
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("dictionary: page user words: %w", err)
	}
	defer rows.Close()
	var out []UserWordEntry
	for rows.Next() {
		var e UserWordEntry
		var status string
		if err := rows.Scan(&e.DictionaryWordID, &e.Lemma, &e.POS, &status); err != nil {
			return nil, fmt.Errorf("dictionary: scan user word: %w", err)
		}
		e.Status = WordStatus(status)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dictionary: iter user words: %w", err)
	}
	return out, nil
}

func (r *sqliteStatusRepo) LearningQueue(ctx context.Context, q DBTX, userID int64, languageCode string, limit int) ([]LearningEntry, error) {
	const stmt = `
		SELECT dw.id, dw.lemma, COALESCE(dw.pos, ''),
		       COALESCE(dw.transcription_ipa, ''), uws.correct_streak
		FROM user_word_status uws
		JOIN dictionary_words dw ON dw.id = uws.dictionary_word_id
		WHERE uws.user_id = ? AND dw.language_code = ? AND uws.status = ?
		ORDER BY uws.updated_at ASC
		LIMIT ?
	`
	rows, err := q.QueryContext(ctx, stmt, userID, languageCode, string(StatusLearning), limit)
	if err != nil {
		return nil, fmt.Errorf("dictionary: learning queue: %w", err)
	}
	defer rows.Close()
	var out []LearningEntry
	for rows.Next() {
		var e LearningEntry
		if err := rows.Scan(&e.DictionaryWordID, &e.Lemma, &e.POS, &e.TranscriptionIPA, &e.CorrectStreak); err != nil {
			return nil, fmt.Errorf("dictionary: scan learning entry: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dictionary: iter learning queue: %w", err)
	}
	return out, nil
}

func (r *sqliteStatusRepo) SampleArticleWords(ctx context.Context, q DBTX, wordIDs []int64) (map[int64]ArticleWordSample, error) {
	out := make(map[int64]ArticleWordSample, len(wordIDs))
	if len(wordIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(wordIDs))
	args := make([]any, 0, len(wordIDs))
	for i, id := range wordIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	// Pick the latest article_words row per dictionary_word_id by max(rowid)
	// — rowid grows monotonically as rows are inserted, so it's a stable
	// recency proxy without needing a created_at column.
	stmt := `
		SELECT aw.dictionary_word_id, aw.surface_form,
		       COALESCE(aw.translation_native, ''),
		       COALESCE(aw.example_target, ''),
		       COALESCE(aw.example_native, '')
		FROM article_words aw
		JOIN (
			SELECT dictionary_word_id, MAX(rowid) AS rid
			FROM article_words
			WHERE dictionary_word_id IN (` + strings.Join(placeholders, ",") + `)
			GROUP BY dictionary_word_id
		) latest ON latest.dictionary_word_id = aw.dictionary_word_id AND latest.rid = aw.rowid
	`
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("dictionary: sample article_words: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s ArticleWordSample
		if err := rows.Scan(&s.DictionaryWordID, &s.SurfaceForm, &s.TranslationNative, &s.ExampleTarget, &s.ExampleNative); err != nil {
			return nil, fmt.Errorf("dictionary: scan article_words sample: %w", err)
		}
		out[s.DictionaryWordID] = s
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dictionary: iter article_words sample: %w", err)
	}
	return out, nil
}

func (r *sqliteStatusRepo) RecordCorrect(ctx context.Context, q DBTX, userID, wordID int64, threshold int) (int, bool, error) {
	const stmt = `
		UPDATE user_word_status
		SET correct_streak = correct_streak + 1,
		    correct_total  = correct_total + 1,
		    status         = CASE WHEN correct_streak + 1 >= ? THEN 'mastered' ELSE status END,
		    updated_at     = CURRENT_TIMESTAMP
		WHERE user_id = ? AND dictionary_word_id = ?
		RETURNING correct_streak, status
	`
	var streak int
	var status string
	if err := q.QueryRowContext(ctx, stmt, threshold, userID, wordID).Scan(&streak, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, ErrNotFound
		}
		return 0, false, fmt.Errorf("dictionary: record correct: %w", err)
	}
	return streak, WordStatus(status) == StatusMastered, nil
}

func (r *sqliteStatusRepo) RecordWrong(ctx context.Context, q DBTX, userID, wordID int64) error {
	const stmt = `
		UPDATE user_word_status
		SET correct_streak = 0,
		    wrong_total    = wrong_total + 1,
		    updated_at     = CURRENT_TIMESTAMP
		WHERE user_id = ? AND dictionary_word_id = ?
	`
	res, err := q.ExecContext(ctx, stmt, userID, wordID)
	if err != nil {
		return fmt.Errorf("dictionary: record wrong: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *sqliteStatusRepo) DeleteWordStatus(ctx context.Context, q DBTX, userID, wordID int64) error {
	const stmt = `DELETE FROM user_word_status WHERE user_id = ? AND dictionary_word_id = ?`
	if _, err := q.ExecContext(ctx, stmt, userID, wordID); err != nil {
		return fmt.Errorf("dictionary: delete word status: %w", err)
	}
	return nil
}

// buildUserWordsQuery composes the count/page queries from the same WHERE
// skeleton: same user/language filter, optional IN-list of statuses, and an
// optional ORDER BY/LIMIT tail. When `tail` is empty, limit/offset are not
// appended (count flow); otherwise tail is `" ORDER BY ... LIMIT ? OFFSET ?"`
// and limit/offset are appended to args.
func buildUserWordsQuery(head, tail string, userID int64, languageCode string, statuses []WordStatus, limit, offset int) (string, []any) {
	var sb strings.Builder
	sb.WriteString(head)
	args := []any{userID, languageCode}
	if len(statuses) > 0 {
		ph := make([]string, len(statuses))
		for i, s := range statuses {
			ph[i] = "?"
			args = append(args, string(s))
		}
		sb.WriteString(" AND uws.status IN (")
		sb.WriteString(strings.Join(ph, ","))
		sb.WriteString(")")
	}
	if tail != "" {
		sb.WriteString(tail)
		args = append(args, limit, offset)
	}
	return sb.String(), args
}

func (r *sqliteStatusRepo) SampleDistractors(
	ctx context.Context, q DBTX,
	userID int64, languageCode string,
	excludeWordID int64, correctAnswer string,
	direction DistractorDirection, n int,
) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	correctNorm := strings.ToLower(strings.TrimSpace(correctAnswer))

	// Overshoot: case-insensitive dedupe shrinks the pool, and ORDER BY RANDOM
	// returns rows we may discard as duplicates of `correctNorm`.
	poolLimit := n * 4
	if poolLimit < 8 {
		poolLimit = 8
	}

	out := make([]string, 0, n)
	seen := make(map[string]struct{}, n)
	add := func(values []string) {
		for _, v := range values {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			key := strings.ToLower(v)
			if key == correctNorm {
				continue
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, v)
			if len(out) >= n {
				return
			}
		}
	}

	fetch := func(query string, args ...any) ([]string, error) {
		rows, err := q.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("dictionary: sample distractors: %w", err)
		}
		defer rows.Close()
		var vs []string
		for rows.Next() {
			var s sql.NullString
			if err := rows.Scan(&s); err != nil {
				return nil, fmt.Errorf("dictionary: scan distractor: %w", err)
			}
			if s.Valid {
				vs = append(vs, s.String)
			}
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("dictionary: iter distractors: %w", err)
		}
		return vs, nil
	}

	var userQuery, globalQuery string
	switch direction {
	case DistractorForeignToNative:
		userQuery = `
			SELECT DISTINCT aw.translation_native
			FROM article_words aw
			JOIN user_word_status uws ON uws.dictionary_word_id = aw.dictionary_word_id
			JOIN dictionary_words dw  ON dw.id = aw.dictionary_word_id
			WHERE uws.user_id = ?
			  AND dw.language_code = ?
			  AND aw.dictionary_word_id != ?
			  AND aw.translation_native IS NOT NULL
			  AND TRIM(aw.translation_native) != ''
			  AND lower(TRIM(aw.translation_native)) != ?
			ORDER BY RANDOM() LIMIT ?
		`
		globalQuery = `
			SELECT DISTINCT aw.translation_native
			FROM article_words aw
			JOIN dictionary_words dw ON dw.id = aw.dictionary_word_id
			WHERE dw.language_code = ?
			  AND aw.dictionary_word_id != ?
			  AND aw.translation_native IS NOT NULL
			  AND TRIM(aw.translation_native) != ''
			  AND lower(TRIM(aw.translation_native)) != ?
			ORDER BY RANDOM() LIMIT ?
		`
	case DistractorNativeToForeign:
		userQuery = `
			SELECT dw.lemma
			FROM user_word_status uws
			JOIN dictionary_words dw ON dw.id = uws.dictionary_word_id
			WHERE uws.user_id = ?
			  AND dw.language_code = ?
			  AND dw.id != ?
			  AND lower(TRIM(dw.lemma)) != ?
			ORDER BY RANDOM() LIMIT ?
		`
		globalQuery = `
			SELECT lemma FROM dictionary_words
			WHERE language_code = ?
			  AND id != ?
			  AND lower(TRIM(lemma)) != ?
			ORDER BY RANDOM() LIMIT ?
		`
	default:
		return nil, fmt.Errorf("dictionary: sample distractors: unknown direction %q", direction)
	}

	vs, err := fetch(userQuery, userID, languageCode, excludeWordID, correctNorm, poolLimit)
	if err != nil {
		return nil, err
	}
	add(vs)
	if len(out) < n {
		vs, err = fetch(globalQuery, languageCode, excludeWordID, correctNorm, poolLimit)
		if err != nil {
			return nil, err
		}
		add(vs)
	}
	return out, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
