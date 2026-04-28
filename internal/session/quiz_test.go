package session_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/nikita/tg-linguine/internal/session"
)

func TestQuiz_RecordAnswerAdvancesAndCounts(t *testing.T) {
	q := session.NewQuiz(0)
	q.Start(1, []session.QuizCard{
		{DictionaryWordID: 10, Lemma: "house", TranslationNative: "дом"},
		{DictionaryWordID: 20, Lemma: "run", TranslationNative: "бежать"},
		{DictionaryWordID: 30, Lemma: "tree", TranslationNative: "дерево"},
	})

	snap, ok := q.RecordAnswer(1, true, false)
	if !ok || snap.Cursor != 1 || snap.Correct != 1 || snap.Wrong != 0 {
		t.Fatalf("after correct: %+v ok=%v", snap, ok)
	}
	snap, _ = q.RecordAnswer(1, false, false)
	if snap.Cursor != 2 || snap.Correct != 1 || snap.Wrong != 1 {
		t.Fatalf("after wrong: %+v", snap)
	}
	snap, _ = q.RecordAnswer(1, true, true) // last card mastered
	if !snap.Done() {
		t.Fatalf("expected Done after 3 answers, got cursor=%d/%d", snap.Cursor, len(snap.Deck))
	}
	if len(snap.Mastered) != 1 || snap.Mastered[0] != "tree" {
		t.Fatalf("mastered list: %v", snap.Mastered)
	}
}

func TestQuiz_RecordAnswerAfterDoneIsNoop(t *testing.T) {
	q := session.NewQuiz(0)
	q.Start(1, []session.QuizCard{{Lemma: "x"}})
	q.RecordAnswer(1, true, false)

	snap, ok := q.RecordAnswer(1, true, false)
	if !ok {
		t.Fatalf("expected session present")
	}
	if snap.Cursor != 1 || snap.Correct != 1 {
		t.Fatalf("counters should not move past end: %+v", snap)
	}
}

func TestQuiz_NoSessionReturnsNotOk(t *testing.T) {
	q := session.NewQuiz(0)
	if _, ok := q.Snapshot(42); ok {
		t.Fatalf("snapshot for unknown user must be !ok")
	}
	if _, ok := q.RecordAnswer(42, true, false); ok {
		t.Fatalf("record for unknown user must be !ok")
	}
}

func TestQuiz_TTLEvictsIdleSession(t *testing.T) {
	q := session.NewQuiz(time.Hour)
	q.Start(1, []session.QuizCard{{Lemma: "x"}})
	if _, ok := q.Snapshot(1); !ok {
		t.Fatalf("session should be present")
	}
	// Force a stale updated timestamp by manipulating via End+Start in the
	// past would require unexported access — instead, use a zero TTL session
	// fast path: re-create with very small TTL and sleep briefly.
	q2 := session.NewQuiz(time.Millisecond)
	q2.Start(1, []session.QuizCard{{Lemma: "x"}})
	time.Sleep(5 * time.Millisecond)
	if _, ok := q2.Snapshot(1); ok {
		t.Fatalf("expected eviction after TTL")
	}
}

func TestQuiz_EndDropsSession(t *testing.T) {
	q := session.NewQuiz(0)
	q.Start(1, []session.QuizCard{{Lemma: "x"}})
	if _, ok := q.End(1); !ok {
		t.Fatalf("end should report ok")
	}
	if _, ok := q.Snapshot(1); ok {
		t.Fatalf("session should be gone after End")
	}
}

func TestQuizCard_PromptAndCorrectAnswer(t *testing.T) {
	c := session.QuizCard{
		Lemma:             "house",
		TranslationNative: "дом",
		Direction:         session.QuizForeignToNative,
	}
	if c.Prompt() != "house" || c.CorrectAnswer() != "дом" {
		t.Fatalf("fwd: prompt=%q answer=%q", c.Prompt(), c.CorrectAnswer())
	}
	c.Direction = session.QuizNativeToForeign
	if c.Prompt() != "дом" || c.CorrectAnswer() != "house" {
		t.Fatalf("bwd: prompt=%q answer=%q", c.Prompt(), c.CorrectAnswer())
	}
}

func TestBuildQuizOptions_IncludesCorrectAndShuffles(t *testing.T) {
	// Pin rng for determinism.
	rng := rand.New(rand.NewSource(42))
	correct := "дом"
	distractors := []string{"бежать", "учить", "дерево"}

	opts, idx := session.BuildQuizOptions(rng, correct, distractors, 4)
	if len(opts) != 4 {
		t.Fatalf("want 4 options, got %d (%v)", len(opts), opts)
	}
	if opts[idx] != correct {
		t.Fatalf("correctIndex %d points to %q, want %q (full %v)", idx, opts[idx], correct, opts)
	}
	// All distractors plus correct must be present.
	want := map[string]bool{correct: false, "бежать": false, "учить": false, "дерево": false}
	for _, o := range opts {
		if _, ok := want[o]; !ok {
			t.Fatalf("unexpected option %q in %v", o, opts)
		}
		want[o] = true
	}
	for k, ok := range want {
		if !ok {
			t.Fatalf("missing option %q in %v", k, opts)
		}
	}
}

func TestBuildQuizOptions_ShortPoolReturnsFewer(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	opts, idx := session.BuildQuizOptions(rng, "house", []string{"tree"}, 4)
	if len(opts) != 2 {
		t.Fatalf("want 2 options when only 1 distractor available, got %d (%v)", len(opts), opts)
	}
	if opts[idx] != "house" {
		t.Fatalf("correct must still be locatable: idx=%d %v", idx, opts)
	}
}

func TestQuiz_RecordSkipAdvancesWithoutCounting(t *testing.T) {
	q := session.NewQuiz(0)
	q.Start(1, []session.QuizCard{
		{DictionaryWordID: 10, Lemma: "house"},
		{DictionaryWordID: 20, Lemma: "run"},
	})

	snap, ok := q.RecordSkip(1)
	if !ok {
		t.Fatal("expected ok=true for existing session")
	}
	if snap.Cursor != 1 {
		t.Fatalf("cursor should advance to 1, got %d", snap.Cursor)
	}
	if snap.Correct != 0 || snap.Wrong != 0 {
		t.Fatalf("skip must not change counters: correct=%d wrong=%d", snap.Correct, snap.Wrong)
	}

	// Second skip reaches Done.
	snap, _ = q.RecordSkip(1)
	if !snap.Done() {
		t.Fatalf("expected Done after skipping all cards, cursor=%d len=%d", snap.Cursor, len(snap.Deck))
	}
	if snap.Correct != 0 || snap.Wrong != 0 {
		t.Fatalf("counters must stay zero after two skips: %+v", snap)
	}
}

func TestQuiz_RecordSkipNoSessionReturnsFalse(t *testing.T) {
	q := session.NewQuiz(0)
	if _, ok := q.RecordSkip(99); ok {
		t.Fatal("RecordSkip for unknown user must return ok=false")
	}
}

func TestPickQuizDirectionAndUIMode_Distribute(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	dirCounts := map[session.QuizDirection]int{}
	uiCounts := map[session.QuizUIMode]int{}
	for i := 0; i < 200; i++ {
		dirCounts[session.PickQuizDirection(rng)]++
		uiCounts[session.PickQuizUIMode(rng)]++
	}
	// Both buckets must be hit at least once over 200 samples — sanity, not stats.
	if dirCounts[session.QuizForeignToNative] == 0 || dirCounts[session.QuizNativeToForeign] == 0 {
		t.Fatalf("direction did not cover both values: %v", dirCounts)
	}
	if uiCounts[session.QuizUIInline] == 0 || uiCounts[session.QuizUIPoll] == 0 {
		t.Fatalf("ui mode did not cover both values: %v", uiCounts)
	}
}
