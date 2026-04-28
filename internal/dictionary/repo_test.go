package dictionary_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nikita/tg-linguine/internal/articles"
	"github.com/nikita/tg-linguine/internal/dictionary"
	"github.com/nikita/tg-linguine/internal/storage"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.RunMigrations(db, slog.New(slog.NewTextHandler(discardWriter{}, nil))); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (telegram_user_id, interface_language) VALUES (?, ?)`, 1001, "ru"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return db
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestDictionary_UpsertLemma_Dedup(t *testing.T) {
	db := newTestDB(t)
	repo := dictionary.NewSQLiteRepository(db)
	ctx := context.Background()

	id1, err := repo.UpsertLemma(ctx, db, dictionary.DictionaryWord{
		LanguageCode: "en", Lemma: "house", POS: "noun", TranscriptionIPA: "/haʊs/",
	})
	if err != nil {
		t.Fatalf("u1: %v", err)
	}
	id2, err := repo.UpsertLemma(ctx, db, dictionary.DictionaryWord{
		LanguageCode: "en", Lemma: "house",
	})
	if err != nil {
		t.Fatalf("u2: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected same id, got %d vs %d", id1, id2)
	}

	// Different language → different row.
	idDe, err := repo.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "de", Lemma: "house"})
	if err != nil {
		t.Fatalf("u3: %v", err)
	}
	if idDe == id1 {
		t.Fatalf("expected distinct id for different language_code")
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dictionary_words`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 dictionary rows, got %d", n)
	}
}

func TestDictionary_TransactionalAtomicity(t *testing.T) {
	db := newTestDB(t)
	artRepo := articles.NewSQLiteRepository(db)
	dict := dictionary.NewSQLiteRepository(db)
	awRepo := dictionary.NewSQLiteArticleWordsRepository(db)
	ctx := context.Background()

	wantErr := errors.New("mid-flight")
	err := articles.WithTx(ctx, db, func(tx *sql.Tx) error {
		a := &articles.Article{
			UserID: 1, SourceURL: "https://x", SourceURLHash: "h", Title: "t", LanguageCode: "en",
		}
		if err := artRepo.Insert(ctx, tx, a); err != nil {
			return err
		}
		wid, err := dict.UpsertLemma(ctx, tx, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "house"})
		if err != nil {
			return err
		}
		if err := awRepo.Insert(ctx, tx, dictionary.ArticleWord{
			ArticleID: a.ID, DictionaryWordID: wid, SurfaceForm: "houses",
		}); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}

	for _, tbl := range []string{"articles", "dictionary_words", "article_words"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		if n != 0 {
			t.Fatalf("rollback failed: %s has %d rows", tbl, n)
		}
	}
}

func TestUserWordStatus_KnownLemmas_FilterByLanguageAndStatus(t *testing.T) {
	db := newTestDB(t)
	dict := dictionary.NewSQLiteRepository(db)
	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)
	ctx := context.Background()

	// Seed a couple of dictionary words in two languages.
	enHouse, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "house"})
	enRun, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "run"})
	enLearn, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "learn"})
	deHaus, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "de", Lemma: "haus"})

	// User 1: house known, run mastered, learn learning, haus known (other language).
	for _, s := range []struct {
		wid int64
		st  dictionary.WordStatus
	}{
		{enHouse, dictionary.StatusKnown},
		{enRun, dictionary.StatusMastered},
		{enLearn, dictionary.StatusLearning},
		{deHaus, dictionary.StatusKnown},
	} {
		if err := statuses.Upsert(ctx, db, dictionary.UserWordStatus{
			UserID: 1, DictionaryWordID: s.wid, Status: s.st,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	got, err := statuses.KnownLemmas(ctx, db, 1, "en")
	if err != nil {
		t.Fatalf("KnownLemmas: %v", err)
	}
	want := []string{"house", "run"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("at %d: got %q want %q (full got=%v)", i, got[i], want[i], got)
		}
	}

	// Other user has no status rows → empty result.
	other, err := statuses.KnownLemmas(ctx, db, 2, "en")
	if err != nil {
		t.Fatalf("KnownLemmas other: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("expected empty for user 2, got %v", other)
	}
}

func TestUserWordStatus_GetMany(t *testing.T) {
	db := newTestDB(t)
	dict := dictionary.NewSQLiteRepository(db)
	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)
	ctx := context.Background()

	w1, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "alpha"})
	w2, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "bravo"})
	w3, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "charlie"})

	if err := statuses.Upsert(ctx, db, dictionary.UserWordStatus{UserID: 1, DictionaryWordID: w1, Status: dictionary.StatusKnown}); err != nil {
		t.Fatal(err)
	}
	if err := statuses.Upsert(ctx, db, dictionary.UserWordStatus{UserID: 1, DictionaryWordID: w2, Status: dictionary.StatusSkipped}); err != nil {
		t.Fatal(err)
	}
	// w3 intentionally has no row.

	got, err := statuses.GetMany(ctx, db, 1, []int64{w1, w2, w3})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if got[w1] != dictionary.StatusKnown || got[w2] != dictionary.StatusSkipped {
		t.Fatalf("unexpected statuses: %v", got)
	}
	if _, ok := got[w3]; ok {
		t.Fatalf("w3 should be absent, got %v", got)
	}

	// Empty input → empty map, no error.
	empty, err := statuses.GetMany(ctx, db, 1, nil)
	if err != nil {
		t.Fatalf("GetMany empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty, got %v", empty)
	}
}

func TestUserWordStatus_Upsert(t *testing.T) {
	db := newTestDB(t)
	dict := dictionary.NewSQLiteRepository(db)
	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)
	ctx := context.Background()

	wid, err := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "tree"})
	if err != nil {
		t.Fatalf("dict: %v", err)
	}

	if err := statuses.Upsert(ctx, db, dictionary.UserWordStatus{
		UserID: 1, DictionaryWordID: wid, Status: dictionary.StatusLearning,
	}); err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	if err := statuses.Upsert(ctx, db, dictionary.UserWordStatus{
		UserID: 1, DictionaryWordID: wid, Status: dictionary.StatusKnown,
	}); err != nil {
		t.Fatalf("upsert2: %v", err)
	}

	got, err := statuses.Get(ctx, db, 1, wid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != dictionary.StatusKnown {
		t.Fatalf("expected known, got %s", got.Status)
	}
}

func TestUserWordStatus_PageUserWords_FiltersAndOrder(t *testing.T) {
	db := newTestDB(t)
	dict := dictionary.NewSQLiteRepository(db)
	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)
	ctx := context.Background()

	enHouse, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "house"})
	enRun, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "run"})
	enLearn, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "learn"})
	enSkip, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "skip"})
	deHaus, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "de", Lemma: "haus"})

	for _, s := range []struct {
		wid int64
		st  dictionary.WordStatus
	}{
		{enHouse, dictionary.StatusKnown},
		{enRun, dictionary.StatusMastered},
		{enLearn, dictionary.StatusLearning},
		{enSkip, dictionary.StatusSkipped},
		{deHaus, dictionary.StatusKnown},
	} {
		if err := statuses.Upsert(ctx, db, dictionary.UserWordStatus{
			UserID: 1, DictionaryWordID: s.wid, Status: s.st,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	tests := []struct {
		name     string
		statuses []dictionary.WordStatus
		want     []string
	}{
		{
			name: "all-tracked",
			statuses: []dictionary.WordStatus{
				dictionary.StatusLearning, dictionary.StatusKnown, dictionary.StatusMastered,
			},
			want: []string{"house", "learn", "run"},
		},
		{
			name:     "learning-only",
			statuses: []dictionary.WordStatus{dictionary.StatusLearning},
			want:     []string{"learn"},
		},
		{
			name:     "known-only",
			statuses: []dictionary.WordStatus{dictionary.StatusKnown},
			want:     []string{"house"},
		},
		{
			name:     "mastered-only",
			statuses: []dictionary.WordStatus{dictionary.StatusMastered},
			want:     []string{"run"},
		},
		{
			name:     "nil-includes-skipped",
			statuses: nil,
			want:     []string{"house", "learn", "run", "skip"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := statuses.PageUserWords(ctx, db, 1, "en", tc.statuses, 100, 0)
			if err != nil {
				t.Fatalf("page: %v", err)
			}
			got := make([]string, 0, len(rows))
			for _, r := range rows {
				got = append(got, r.Lemma)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("at %d: got %q want %q (full got=%v)", i, got[i], tc.want[i], got)
				}
			}
			n, err := statuses.CountUserWords(ctx, db, 1, "en", tc.statuses)
			if err != nil {
				t.Fatalf("count: %v", err)
			}
			if n != len(tc.want) {
				t.Fatalf("count mismatch: got %d want %d", n, len(tc.want))
			}
		})
	}

	// Pagination: limit 2 should still return alphabetical order.
	rows, err := statuses.PageUserWords(ctx, db, 1, "en", nil, 2, 0)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(rows) != 2 || rows[0].Lemma != "house" || rows[1].Lemma != "learn" {
		t.Fatalf("page1 unexpected: %+v", rows)
	}
	rows, err = statuses.PageUserWords(ctx, db, 1, "en", nil, 2, 2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(rows) != 2 || rows[0].Lemma != "run" || rows[1].Lemma != "skip" {
		t.Fatalf("page2 unexpected: %+v", rows)
	}
}

func TestUserWordStatus_RecordCorrect_ThresholdPromotion(t *testing.T) {
	db := newTestDB(t)
	dict := dictionary.NewSQLiteRepository(db)
	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)
	ctx := context.Background()

	wid, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "tree"})
	if err := statuses.Upsert(ctx, db, dictionary.UserWordStatus{
		UserID: 1, DictionaryWordID: wid, Status: dictionary.StatusLearning,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Two correct → still learning.
	for i := 1; i <= 2; i++ {
		streak, mastered, err := statuses.RecordCorrect(ctx, db, 1, wid, 3)
		if err != nil {
			t.Fatalf("rc%d: %v", i, err)
		}
		if streak != i || mastered {
			t.Fatalf("after rc%d: streak=%d mastered=%v", i, streak, mastered)
		}
	}

	// One wrong resets streak; status stays learning.
	if err := statuses.RecordWrong(ctx, db, 1, wid); err != nil {
		t.Fatalf("rw: %v", err)
	}
	got, err := statuses.Get(ctx, db, 1, wid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CorrectStreak != 0 {
		t.Fatalf("streak after wrong: %d", got.CorrectStreak)
	}
	if got.Status != dictionary.StatusLearning {
		t.Fatalf("status after wrong: %s", got.Status)
	}
	if got.WrongTotal != 1 {
		t.Fatalf("wrong_total: %d", got.WrongTotal)
	}

	// Three correct in a row → mastered on the third.
	for i := 1; i <= 2; i++ {
		_, mastered, err := statuses.RecordCorrect(ctx, db, 1, wid, 3)
		if err != nil {
			t.Fatalf("rc%d (post-reset): %v", i, err)
		}
		if mastered {
			t.Fatalf("mastered too early at %d", i)
		}
	}
	streak, mastered, err := statuses.RecordCorrect(ctx, db, 1, wid, 3)
	if err != nil {
		t.Fatalf("rc3 (post-reset): %v", err)
	}
	if streak != 3 || !mastered {
		t.Fatalf("expected promotion at streak=3, got streak=%d mastered=%v", streak, mastered)
	}
	got, err = statuses.Get(ctx, db, 1, wid)
	if err != nil {
		t.Fatalf("get final: %v", err)
	}
	if got.Status != dictionary.StatusMastered {
		t.Fatalf("final status: %s", got.Status)
	}
}

func TestUserWordStatus_SampleDistractors_ForeignToNative(t *testing.T) {
	db := newTestDB(t)
	artRepo := articles.NewSQLiteRepository(db)
	dict := dictionary.NewSQLiteRepository(db)
	awRepo := dictionary.NewSQLiteArticleWordsRepository(db)
	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)
	ctx := context.Background()

	// Seed: user 1 has 4 English words tracked; each has a translation in
	// article_words. We'll ask for distractors of "house" (translation "дом").
	a := &articles.Article{
		UserID: 1, SourceURL: "https://x", SourceURLHash: "h", Title: "t", LanguageCode: "en",
	}
	if err := artRepo.Insert(ctx, db, a); err != nil {
		t.Fatalf("article: %v", err)
	}
	type seed struct {
		lemma, translation string
	}
	rows := []seed{
		{"house", "дом"},
		{"run", "бежать"},
		{"learn", "учить"},
		{"tree", "дерево"},
	}
	wids := make(map[string]int64, len(rows))
	for _, r := range rows {
		wid, err := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: r.lemma})
		if err != nil {
			t.Fatalf("dict %s: %v", r.lemma, err)
		}
		wids[r.lemma] = wid
		if err := awRepo.Insert(ctx, db, dictionary.ArticleWord{
			ArticleID: a.ID, DictionaryWordID: wid, SurfaceForm: r.lemma, TranslationNative: r.translation,
		}); err != nil {
			t.Fatalf("aw %s: %v", r.lemma, err)
		}
		if err := statuses.Upsert(ctx, db, dictionary.UserWordStatus{
			UserID: 1, DictionaryWordID: wid, Status: dictionary.StatusLearning,
		}); err != nil {
			t.Fatalf("status %s: %v", r.lemma, err)
		}
	}

	got, err := statuses.SampleDistractors(ctx, db, 1, "en",
		wids["house"], "дом", dictionary.DistractorForeignToNative, 3)
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 distractors, got %d (%v)", len(got), got)
	}
	seen := map[string]bool{}
	for _, v := range got {
		if v == "дом" {
			t.Fatalf("must not return correct answer: %v", got)
		}
		if seen[v] {
			t.Fatalf("duplicate distractor %q in %v", v, got)
		}
		seen[v] = true
	}
	want := map[string]bool{"бежать": true, "учить": true, "дерево": true}
	for _, v := range got {
		if !want[v] {
			t.Fatalf("unexpected distractor %q (want one of %v)", v, want)
		}
	}
}

func TestUserWordStatus_SampleDistractors_NativeToForeign_BackfillsFromGlobal(t *testing.T) {
	db := newTestDB(t)
	dict := dictionary.NewSQLiteRepository(db)
	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)
	ctx := context.Background()

	// User 1 only tracks one English word; the global pool has more lemmas
	// (seeded but not attached to any user_word_status row) — backfill must
	// kick in to satisfy n=3.
	target, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "house"})
	if err := statuses.Upsert(ctx, db, dictionary.UserWordStatus{
		UserID: 1, DictionaryWordID: target, Status: dictionary.StatusLearning,
	}); err != nil {
		t.Fatalf("status: %v", err)
	}
	// Global-only words (no user_word_status row).
	for _, lemma := range []string{"alpha", "bravo", "charlie", "delta"} {
		if _, err := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: lemma}); err != nil {
			t.Fatalf("dict %s: %v", lemma, err)
		}
	}
	// A word in another language must not leak in.
	if _, err := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "de", Lemma: "haus"}); err != nil {
		t.Fatalf("de: %v", err)
	}

	got, err := statuses.SampleDistractors(ctx, db, 1, "en",
		target, "house", dictionary.DistractorNativeToForeign, 3)
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3, got %d (%v)", len(got), got)
	}
	for _, v := range got {
		if v == "house" {
			t.Fatalf("must not return correct lemma: %v", got)
		}
		if v == "haus" {
			t.Fatalf("must not leak from other language: %v", got)
		}
	}
}

func TestUserWordStatus_SampleDistractors_ExcludesCorrectAnswerCaseInsensitive(t *testing.T) {
	db := newTestDB(t)
	dict := dictionary.NewSQLiteRepository(db)
	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)
	ctx := context.Background()

	// Pool intentionally contains a casing variant of the correct answer.
	target, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "Run"})
	if _, err := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "run"}); err != nil {
		t.Fatalf("dup: %v", err)
	}
	if _, err := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "walk"}); err != nil {
		t.Fatalf("walk: %v", err)
	}

	got, err := statuses.SampleDistractors(ctx, db, 1, "en",
		target, "Run", dictionary.DistractorNativeToForeign, 2)
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	for _, v := range got {
		if strings.EqualFold(v, "run") {
			t.Fatalf("must filter casing variants of correct answer: %v", got)
		}
	}
}

func TestUserWordStatus_SampleDistractors_ReturnsFewerWhenPoolTooSmall(t *testing.T) {
	db := newTestDB(t)
	dict := dictionary.NewSQLiteRepository(db)
	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)
	ctx := context.Background()

	// Only one other lemma exists in the language: at most 1 distractor possible.
	target, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "house"})
	if _, err := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "tree"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := statuses.SampleDistractors(ctx, db, 1, "en",
		target, "house", dictionary.DistractorNativeToForeign, 3)
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if len(got) != 1 || got[0] != "tree" {
		t.Fatalf("expected exactly [tree], got %v", got)
	}
}

func TestUserWordStatus_SampleArticleWords_LatestPerWord(t *testing.T) {
	db := newTestDB(t)
	artRepo := articles.NewSQLiteRepository(db)
	dict := dictionary.NewSQLiteRepository(db)
	awRepo := dictionary.NewSQLiteArticleWordsRepository(db)
	statuses := dictionary.NewSQLiteUserWordStatusRepository(db)
	ctx := context.Background()

	wid, _ := dict.UpsertLemma(ctx, db, dictionary.DictionaryWord{LanguageCode: "en", Lemma: "house"})

	for i, surface := range []string{"houses", "houses (old)", "houses (new)"} {
		a := &articles.Article{
			UserID: 1, SourceURL: "https://x/" + strconv.Itoa(i), SourceURLHash: "h" + strconv.Itoa(i),
			Title: "t", LanguageCode: "en",
		}
		if err := artRepo.Insert(ctx, db, a); err != nil {
			t.Fatalf("seed article %d: %v", i, err)
		}
		if err := awRepo.Insert(ctx, db, dictionary.ArticleWord{
			ArticleID: a.ID, DictionaryWordID: wid, SurfaceForm: surface,
			ExampleTarget: "e" + strconv.Itoa(i),
		}); err != nil {
			t.Fatalf("seed aw %d: %v", i, err)
		}
	}

	got, err := statuses.SampleArticleWords(ctx, db, []int64{wid})
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	s, ok := got[wid]
	if !ok {
		t.Fatalf("expected sample for wid=%d", wid)
	}
	// rowid grows with insertion order, so the "(new)" surface should win.
	if s.SurfaceForm != "houses (new)" || s.ExampleTarget != "e2" {
		t.Fatalf("expected latest sample, got %+v", s)
	}
}
